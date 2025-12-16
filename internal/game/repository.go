package game

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

//
// -----------------------------------------------------------------------------
// Game creation & loading
// -----------------------------------------------------------------------------

// CreateGame inserts a game row and its game_players, then returns the full GameState.
func (r *Repository) CreateGame(ctx context.Context, req CreateGameRequest) (GameState, error) {
	if len(req.PlayerIDs) == 0 {
		return GameState{}, errors.New("at least one player is required")
	}

	mode := strings.TrimSpace(req.Config.Mode)
	if mode == "" {
		return GameState{}, errors.New("mode is required")
	}
	if req.Config.Legs <= 0 {
		return GameState{}, errors.New("legs must be > 0")
	}
	if req.Config.Sets <= 0 {
		return GameState{}, errors.New("sets must be > 0")
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return GameState{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var gameID string
	var createdAt time.Time

	err = tx.QueryRow(ctx, `
INSERT INTO games (mode, starting_score, legs, sets, double_out)
VALUES ($1, $2, $3, $4, $5)
RETURNING id::text, created_at;
`, mode, req.Config.StartingScore, req.Config.Legs, req.Config.Sets, req.Config.DoubleOut).
		Scan(&gameID, &createdAt)
	if err != nil {
		return GameState{}, err
	}

	for i, pid := range req.PlayerIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO game_players (game_id, player_id, seat)
VALUES ($1, $2, $3);
`, gameID, pid, i+1); err != nil {
			return GameState{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return GameState{}, err
	}

	state, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}
	if err := r.syncGameStatus(ctx, &state); err != nil {
		return GameState{}, err
	}

	return state, nil
}

// GetGame loads a game and returns a GameState.
func (r *Repository) GetGame(ctx context.Context, gameID string) (GameState, error) {
	state, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}
	if err := r.syncGameStatus(ctx, &state); err != nil {
		return GameState{}, err
	}
	return state, nil
}

// ListGames returns recent games (without history/scores) for the history view.
func (r *Repository) ListGames(ctx context.Context, limit int) ([]Game, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.db.Query(ctx, `
SELECT id::text, mode, starting_score, legs, sets, double_out, status, created_at, winner_id
FROM games
ORDER BY created_at DESC
LIMIT $1;
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games := make([]Game, 0)
	for rows.Next() {
		var g Game
		var startingScore *int
		var winnerID *string

		if err := rows.Scan(
			&g.ID,
			&g.Config.Mode,
			&startingScore,
			&g.Config.Legs,
			&g.Config.Sets,
			&g.Config.DoubleOut,
			&g.Status,
			&g.CreatedAt,
			&winnerID,
		); err != nil {
			return nil, err
		}

		g.Config.StartingScore = startingScore
		g.WinnerID = winnerID
		games = append(games, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load players for each game (simple N+1, fine for personal use).
	for i := range games {
		players, err := r.loadPlayersForGame(ctx, games[i].ID)
		if err != nil {
			return nil, err
		}
		games[i].Players = players
	}

	return games, nil
}

//
// -----------------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------------

// getGameState reads from games + game_players + players + throws
// and constructs a GameState with computed scores & current player.
func (r *Repository) getGameState(ctx context.Context, gameID string) (GameState, error) {
	var state GameState
	var startingScore *int

	// Load game row
	err := r.db.QueryRow(ctx, `
SELECT id::text, mode, starting_score, legs, sets, double_out, status, created_at, winner_id
FROM games
WHERE id = $1;
`, gameID).Scan(
		&state.ID,
		&state.Config.Mode,
		&startingScore,
		&state.Config.Legs,
		&state.Config.Sets,
		&state.Config.DoubleOut,
		&state.Status,
		&state.CreatedAt,
		&state.WinnerID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GameState{}, fmt.Errorf("game not found")
		}
		return GameState{}, err
	}
	state.Config.StartingScore = startingScore

	// Load players (seating order)
	players, err := r.loadPlayersForGame(ctx, state.ID)
	if err != nil {
		return GameState{}, err
	}
	state.Players = players

	// Load throws history
	trows, err := r.db.Query(ctx, `
SELECT id::text, game_id::text, player_id::text, visit_score, darts_thrown, created_at
FROM throws
WHERE game_id = $1
ORDER BY created_at ASC, id ASC;
`, state.ID)
	if err != nil {
		return GameState{}, err
	}
	defer trows.Close()

	state.History = make([]Throw, 0)
	for trows.Next() {
		var t Throw
		if err := trows.Scan(
			&t.ID,
			&t.GameID,
			&t.PlayerID,
			&t.VisitScore,
			&t.DartsThrown,
			&t.CreatedAt,
		); err != nil {
			return GameState{}, err
		}
		state.History = append(state.History, t)
	}
	if err := trows.Err(); err != nil {
		return GameState{}, err
	}

	// Compute scores + currentPlayer based on mode & history
	r.computeScores(&state)

	return state, nil
}

// loadPlayersForGame loads the players for a single game (in seat order).
func (r *Repository) loadPlayersForGame(ctx context.Context, gameID string) ([]GamePlayer, error) {
	rows, err := r.db.Query(ctx, `
SELECT p.id::text, p.name, gp.seat
FROM game_players gp
JOIN players p ON p.id = gp.player_id
WHERE gp.game_id = $1
ORDER BY gp.seat ASC;
`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	players := make([]GamePlayer, 0)
	for rows.Next() {
		var gp GamePlayer
		if err := rows.Scan(&gp.ID, &gp.Name, &gp.Seat); err != nil {
			return nil, err
		}
		players = append(players, gp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return players, nil
}

//
// -----------------------------------------------------------------------------
// Throws / scoring (X01 + undo)
// -----------------------------------------------------------------------------

// AddThrow inserts a new throw and returns the updated GameState.
func (r *Repository) AddThrow(ctx context.Context, gameID string, req CreateThrowRequest) (GameState, error) {
	req.PlayerID = strings.TrimSpace(req.PlayerID)
	if req.PlayerID == "" {
		return GameState{}, errors.New("playerId is required")
	}
	if req.DartsThrown < 1 || req.DartsThrown > 3 {
		return GameState{}, errors.New("dartsThrown must be between 1 and 3")
	}
	if req.VisitScore < 0 || req.VisitScore > 180 {
		return GameState{}, errors.New("visitScore must be between 0 and 180")
	}

	// Load current state to validate membership & turn / finished state.
	stateBefore, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}

	if stateBefore.Status == "finished" {
		return GameState{}, errors.New("game is already finished")
	}

	// Ensure player is part of this game
	playerInGame := false
	for _, p := range stateBefore.Players {
		if p.ID == req.PlayerID {
			playerInGame = true
			break
		}
	}
	if !playerInGame {
		return GameState{}, errors.New("player is not part of this game")
	}

	// Enforce turn order: only currentPlayerId is allowed to throw
	if stateBefore.CurrentPlayerID != "" && stateBefore.CurrentPlayerID != req.PlayerID {
		return GameState{}, fmt.Errorf("not this player's turn")
	}

	// Insert throw
	_, err = r.db.Exec(ctx, `
INSERT INTO throws (game_id, player_id, visit_score, darts_thrown)
VALUES ($1, $2, $3, $4);
`, gameID, req.PlayerID, req.VisitScore, req.DartsThrown)
	if err != nil {
		return GameState{}, err
	}

	// Reload full state after the new throw
	stateAfter, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}
	if err := r.syncGameStatus(ctx, &stateAfter); err != nil {
		return GameState{}, err
	}

	return stateAfter, nil
}

// UndoLastThrow deletes the most recent throw for a game and returns the updated GameState.
func (r *Repository) UndoLastThrow(ctx context.Context, gameID string) (GameState, error) {
	// Find last throw
	var lastThrowID string
	err := r.db.QueryRow(ctx, `
SELECT id::text
FROM throws
WHERE game_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;
`, gameID).Scan(&lastThrowID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GameState{}, errors.New("no throws to undo")
		}
		return GameState{}, err
	}

	// Delete it
	if _, err := r.db.Exec(ctx, `
DELETE FROM throws
WHERE id = $1;
`, lastThrowID); err != nil {
		return GameState{}, err
	}

	// Reload state after undo
	state, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}
	if err := r.syncGameStatus(ctx, &state); err != nil {
		return GameState{}, err
	}

	return state, nil
}

// computeScores fills state.Scores and state.CurrentPlayerId based on mode + history.
// X01 rules implemented:
//   - Starting score (default 501 if not provided)
//   - Bust if score < 0
//   - If double-out is enabled, leaving 1 is a bust (can't finish on 1)
//   - Reaching 0 is a finish (we don't yet validate last dart as double).
func (r *Repository) computeScores(state *GameState) {
	if len(state.Players) == 0 {
		state.Scores = []PlayerScore{}
		state.CurrentPlayerID = ""
		return
	}

	// Map playerID → index
	playerIndex := make(map[string]int, len(state.Players))
	for i, p := range state.Players {
		playerIndex[p.ID] = i
	}

	// Initialize scores with one entry per player.
	scores := make([]PlayerScore, len(state.Players))
	for i, p := range state.Players {
		scores[i] = PlayerScore{
			PlayerID: p.ID,
		}
	}

	// Default starting score
	start := 501
	if state.Config.StartingScore != nil {
		start = *state.Config.StartingScore
	}

	remaining := make([]int, len(state.Players))

	if state.Config.Mode == "X01" {
		for i := range remaining {
			remaining[i] = start
			val := remaining[i]
			scores[i].Remaining = &val
		}

		for _, t := range state.History {
			idx, ok := playerIndex[t.PlayerID]
			if !ok {
				continue
			}

			cur := remaining[idx]
			cand := cur - t.VisitScore

			// Bust rules:
			// - If result < 0, bust (ignore this visit)
			// - If double-out and result == 1, bust
			if cand < 0 {
				continue
			}
			if state.Config.DoubleOut && cand == 1 {
				continue
			}

			// Accept the visit
			remaining[idx] = cand
			visit := t.VisitScore
			scores[idx].LastVisit = &visit
			val := remaining[idx]
			scores[idx].Remaining = &val
		}
	}

	// Determine current player based on last throw; if no throws, first player.
	if len(state.History) == 0 {
		state.CurrentPlayerID = state.Players[0].ID
	} else {
		last := state.History[len(state.History)-1]
		lastIdx, ok := playerIndex[last.PlayerID]
		if !ok {
			state.CurrentPlayerID = state.Players[0].ID
		} else {
			nextIdx := (lastIdx + 1) % len(state.Players)
			state.CurrentPlayerID = state.Players[nextIdx].ID
		}
	}

	state.Scores = scores
}

// syncGameStatus updates the games.status and games.winner_id fields
// based on scores + history:
// - finished: any player has remaining == 0
// - in_progress: at least one throw and no winner
// - pending: no throws
func (r *Repository) syncGameStatus(ctx context.Context, state *GameState) error {
	// Determine winner (if any) from scores.
	var winnerID *string
	for _, s := range state.Scores {
		if s.Remaining != nil && *s.Remaining == 0 {
			id := s.PlayerID
			winnerID = &id
			break
		}
	}

	var newStatus string
	if winnerID != nil {
		newStatus = "finished"
	} else if len(state.History) > 0 {
		newStatus = "in_progress"
	} else {
		newStatus = "pending"
	}

	// Check if status or winner changed.
	sameWinner := false
	if state.WinnerID == nil && winnerID == nil {
		sameWinner = true
	} else if state.WinnerID != nil && winnerID != nil && *state.WinnerID == *winnerID {
		sameWinner = true
	}

	if newStatus == state.Status && sameWinner {
		return nil
	}

	if _, err := r.db.Exec(ctx, `
UPDATE games
SET status = $1, winner_id = $2
WHERE id = $3;
`, newStatus, winnerID, state.ID); err != nil {
		return err
	}

	state.Status = newStatus
	state.WinnerID = winnerID
	return nil
}

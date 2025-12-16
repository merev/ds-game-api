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

// -----------------------------------------------------------------------------
// Game creation & loading
// -----------------------------------------------------------------------------

// CreateGame inserts a game row and its game_players, then returns the full GameState.
func (r *Repository) CreateGame(ctx context.Context, req CreateGameRequest) (GameState, error) {
	if len(req.PlayerIDs) == 0 {
		return GameState{}, errors.New("at least one player is required")
	}

	// Read config from nested struct
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
	// Ensure rollback if we return before Commit
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

	// Now load the full game state with players and (currently empty) history.
	return r.getGameState(ctx, gameID)
}

// GetGame loads a game and returns a GameState.
func (r *Repository) GetGame(ctx context.Context, gameID string) (GameState, error) {
	return r.getGameState(ctx, gameID)
}

// getGameState reads from games + game_players + players + throws
// and constructs a GameState with computed scores & current player.
func (r *Repository) getGameState(ctx context.Context, gameID string) (GameState, error) {
	var state GameState
	var startingScore *int

	// Load game row
	err := r.db.QueryRow(ctx, `
SELECT id::text, mode, starting_score, legs, sets, double_out, status, created_at
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GameState{}, fmt.Errorf("game not found")
		}
		return GameState{}, err
	}
	state.Config.StartingScore = startingScore

	// Load players (seating order)
	rows, err := r.db.Query(ctx, `
SELECT p.id::text, p.name, gp.seat
FROM game_players gp
JOIN players p ON p.id = gp.player_id
WHERE gp.game_id = $1
ORDER BY gp.seat ASC;
`, state.ID)
	if err != nil {
		return GameState{}, err
	}
	defer rows.Close()

	state.Players = make([]GamePlayer, 0)
	for rows.Next() {
		var gp GamePlayer
		if err := rows.Scan(&gp.ID, &gp.Name, &gp.Seat); err != nil {
			return GameState{}, err
		}
		state.Players = append(state.Players, gp)
	}
	if err := rows.Err(); err != nil {
		return GameState{}, err
	}

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

// -----------------------------------------------------------------------------
// Throws / scoring
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

	// Load current state to validate player & turn
	state, err := r.getGameState(ctx, gameID)
	if err != nil {
		return GameState{}, err
	}

	// Ensure player is part of this game
	playerInGame := false
	for _, p := range state.Players {
		if p.ID == req.PlayerID {
			playerInGame = true
			break
		}
	}
	if !playerInGame {
		return GameState{}, errors.New("player is not part of this game")
	}

	// Enforce turn order: only currentPlayerId is allowed to throw
	if state.CurrentPlayerID != "" && state.CurrentPlayerID != req.PlayerID {
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
	return r.getGameState(ctx, gameID)
}

// computeScores fills state.Scores and state.CurrentPlayerId based on mode + history.
// For now we implement a simple X01-like scoring model and keep other modes minimal.
func (r *Repository) computeScores(state *GameState) {
	// Default: if no players, nothing to do.
	if len(state.Players) == 0 {
		state.Scores = []PlayerScore{}
		state.CurrentPlayerID = ""
		return
	}

	// Map players by ID and remember order.
	playerIndex := make(map[string]int, len(state.Players))
	for i, p := range state.Players {
		playerIndex[p.ID] = i
	}

	// Initialize scores slice with entries for each player.
	scores := make([]PlayerScore, len(state.Players))
	for i, p := range state.Players {
		scores[i] = PlayerScore{
			PlayerID: p.ID,
		}
	}

	// Starting score for X01; if not specified, default to 501.
	start := 0
	if state.Config.StartingScore != nil {
		start = *state.Config.StartingScore
	} else {
		start = 501
	}

	// Only apply scoring if mode is X01.
	// For other modes we just keep remaining undefined.
	if state.Config.Mode == "X01" {
		remaining := make([]int, len(state.Players))
		for i := range remaining {
			remaining[i] = start
			// initialize remaining in scores as well
			val := remaining[i]
			scores[i].Remaining = &val
		}

		// Process throws in order; simple model: apply visitScore unless it would overshoot < 0.
		// (We don't implement full double-out / bust rules yet.)
		for _, t := range state.History {
			idx, ok := playerIndex[t.PlayerID]
			if !ok {
				continue // throw from unknown player? skip
			}

			cur := remaining[idx]
			candidate := cur - t.VisitScore
			if candidate < 0 {
				// Bust: ignore visit, score stays.
				continue
			}

			remaining[idx] = candidate
			visit := t.VisitScore
			scores[idx].LastVisit = &visit

			// Update remaining pointer
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
			// Fallback
			state.CurrentPlayerID = state.Players[0].ID
		} else {
			nextIdx := (lastIdx + 1) % len(state.Players)
			state.CurrentPlayerID = state.Players[nextIdx].ID
		}
	}

	state.Scores = scores
}

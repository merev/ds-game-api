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

	// Now load the full game state with players.
	return r.getGameState(ctx, gameID)
}

// GetGame loads a game and returns a GameState struct.
func (r *Repository) GetGame(ctx context.Context, gameID string) (GameState, error) {
	return r.getGameState(ctx, gameID)
}

// getGameState is an internal helper that reads from games + game_players + players
// and constructs a GameState with empty scores/history for now.
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

	// Load players for this game
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

	// ---- Temporary scoring scaffolding ----
	// Until we implement real scoring / throws, we:
	// - create an empty score object per player
	// - set current player to the first player
	state.Scores = make([]PlayerScore, 0, len(state.Players))
	for _, p := range state.Players {
		state.Scores = append(state.Scores, PlayerScore{
			PlayerID: p.ID,
			// Remaining / LastVisit / LastThreeDarts left nil/zero for now
		})
	}

	if len(state.Players) > 0 {
		state.CurrentPlayerID = state.Players[0].ID
	}

	// No history yet, we haven't implemented throws.
	state.History = make([]Throw, 0)

	return state, nil
}

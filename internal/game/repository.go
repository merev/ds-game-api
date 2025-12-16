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

// CreateGame inserts a game row and its game_players, then returns the full Game.
func (r *Repository) CreateGame(ctx context.Context, req CreateGameRequest) (Game, error) {
	if len(req.PlayerIDs) == 0 {
		return Game{}, errors.New("at least one player is required")
	}

	// Read config from nested struct
	mode := strings.TrimSpace(req.Config.Mode)
	if mode == "" {
		return Game{}, errors.New("mode is required")
	}
	if req.Config.Legs <= 0 {
		return Game{}, errors.New("legs must be > 0")
	}
	if req.Config.Sets <= 0 {
		return Game{}, errors.New("sets must be > 0")
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Game{}, err
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
		return Game{}, err
	}

	for i, pid := range req.PlayerIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO game_players (game_id, player_id, seat)
VALUES ($1, $2, $3);
`, gameID, pid, i+1); err != nil {
			return Game{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Game{}, err
	}

	// Now load the full game with players.
	return r.GetGame(ctx, gameID)
}

// GetGame loads a game row and its players and returns a Game struct.
func (r *Repository) GetGame(ctx context.Context, gameID string) (Game, error) {
	var g Game
	var startingScore *int

	err := r.db.QueryRow(ctx, `
SELECT id::text, mode, starting_score, legs, sets, double_out, status, created_at
FROM games
WHERE id = $1;
`, gameID).Scan(
		&g.ID,
		&g.Config.Mode,
		&startingScore,
		&g.Config.Legs,
		&g.Config.Sets,
		&g.Config.DoubleOut,
		&g.Status,
		&g.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Game{}, fmt.Errorf("game not found")
		}
		return Game{}, err
	}
	g.Config.StartingScore = startingScore

	// Load players for this game
	rows, err := r.db.Query(ctx, `
SELECT p.id::text, p.name, gp.seat
FROM game_players gp
JOIN players p ON p.id = gp.player_id
WHERE gp.game_id = $1
ORDER BY gp.seat ASC;
`, g.ID)
	if err != nil {
		return Game{}, err
	}
	defer rows.Close()

	g.Players = make([]GamePlayer, 0)
	for rows.Next() {
		var gp GamePlayer
		if err := rows.Scan(&gp.ID, &gp.Name, &gp.Seat); err != nil {
			return Game{}, err
		}
		g.Players = append(g.Players, gp)
	}

	if err := rows.Err(); err != nil {
		return Game{}, err
	}

	return g, nil
}

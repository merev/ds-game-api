package database

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(dsn string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(ctx); err != nil {
		return nil, err
	}

	return db, nil
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	const enablePgcrypto = `CREATE EXTENSION IF NOT EXISTS pgcrypto;`

	const gamesTable = `
CREATE TABLE IF NOT EXISTS games (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode           TEXT NOT NULL,
    starting_score INT,
    legs           INT NOT NULL,
    sets           INT NOT NULL,
    double_out     BOOLEAN NOT NULL DEFAULT TRUE,
    status         TEXT NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

	const gamePlayersTable = `
CREATE TABLE IF NOT EXISTS game_players (
    game_id   UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    player_id UUID NOT NULL REFERENCES players(id) ON DELETE RESTRICT,
    seat      INT NOT NULL,
    PRIMARY KEY (game_id, player_id)
);
`

	const throwsTable = `
CREATE TABLE IF NOT EXISTS throws (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id      UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    player_id    UUID NOT NULL REFERENCES players(id) ON DELETE RESTRICT,
    visit_score  INT NOT NULL,
    darts_thrown INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

	const addWinnerColumn = `
ALTER TABLE games
ADD COLUMN IF NOT EXISTS winner_id UUID REFERENCES players(id);
`

	if _, err := db.Exec(ctx, enablePgcrypto); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, gamesTable); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, gamePlayersTable); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, throwsTable); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, addWinnerColumn); err != nil {
		return err
	}

	log.Println("game-api migrations applied")
	return nil
}

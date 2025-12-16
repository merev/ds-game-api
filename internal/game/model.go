package game

import "time"

// GameConfig mirrors the frontend config object structure.
type GameConfig struct {
	Mode          string `json:"mode"`                    // "X01", "Cricket", etc.
	StartingScore *int   `json:"startingScore,omitempty"` // only for X01
	Legs          int    `json:"legs"`
	Sets          int    `json:"sets"`
	DoubleOut     bool   `json:"doubleOut"`
}

// A player attached to a game, with fixed seating order.
type GamePlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Seat int    `json:"seat"`
}

// Game is returned to the frontend when creating or loading a game.
type Game struct {
	ID        string       `json:"id"`
	Config    GameConfig   `json:"config"`
	Status    string       `json:"status"`
	Players   []GamePlayer `json:"players"`
	CreatedAt time.Time    `json:"createdAt"`
}

// CreateGameRequest matches EXACTLY what the frontend sends:
//
//	{
//	  "config": { ... },
//	  "playerIds": ["uuid1", "uuid2"]
//	}
type CreateGameRequest struct {
	Config    GameConfig `json:"config"`
	PlayerIDs []string   `json:"playerIds"`
}

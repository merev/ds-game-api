package game

import "time"

// GameConfig mirrors frontend config structure.
type GameConfig struct {
	Mode          string `json:"mode"`                    // 'X01', 'Cricket', etc.
	StartingScore *int   `json:"startingScore,omitempty"` // only for X01
	Legs          int    `json:"legs"`
	Sets          int    `json:"sets"`
	DoubleOut     bool   `json:"doubleOut"`
}

type GamePlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Seat int    `json:"seat"`
}

type Game struct {
	ID        string       `json:"id"`
	Config    GameConfig   `json:"config"`
	Status    string       `json:"status"`
	Players   []GamePlayer `json:"players"`
	CreatedAt time.Time    `json:"createdAt"`
}

// CreateGameRequest is the body we expect on POST /api/games.
type CreateGameRequest struct {
	Mode          string   `json:"mode"`
	StartingScore *int     `json:"startingScore"`
	Legs          int      `json:"legs"`
	Sets          int      `json:"sets"`
	DoubleOut     bool     `json:"doubleOut"`
	PlayerIDs     []string `json:"players"` // list of player IDs
}

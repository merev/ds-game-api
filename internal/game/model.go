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

type PlayerScore struct {
	PlayerID  string `json:"playerId"`
	Remaining *int   `json:"remaining,omitempty"`
	LastVisit *int   `json:"lastVisit,omitempty"`
	LastThree []int  `json:"lastThreeDarts,omitempty"`
}

type Throw struct {
	ID          string    `json:"id"`
	GameID      string    `json:"gameId"`
	PlayerID    string    `json:"playerId"`
	VisitScore  int       `json:"visitScore"`
	DartsThrown int       `json:"dartsThrown"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CreateThrowRequest struct {
	PlayerID    string `json:"playerId"`
	VisitScore  int    `json:"visitScore"`
	DartsThrown int    `json:"dartsThrown"`
}

type GameState struct {
	ID              string        `json:"id"`
	Config          GameConfig    `json:"config"`
	Status          string        `json:"status"`
	Players         []GamePlayer  `json:"players"`
	Scores          []PlayerScore `json:"scores"`
	CurrentPlayerID string        `json:"currentPlayerId"`
	History         []Throw       `json:"history"`
	CreatedAt       time.Time     `json:"createdAt"`
}

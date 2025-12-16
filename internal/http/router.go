package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/merev/ds-game-api/internal/game"
)

func NewRouter(gh *game.Handler) http.Handler {
	r := chi.NewRouter()

	r.Route("/api", func(api chi.Router) {
		api.Post("/games", gh.CreateGame)  // POST /api/games
		api.Get("/games/{id}", gh.GetGame) // GET /api/games/:id
	})

	return r
}

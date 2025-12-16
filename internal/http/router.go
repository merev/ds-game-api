package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/merev/ds-game-api/internal/game"
)

func NewRouter(gh *game.Handler) http.Handler {
	r := chi.NewRouter()

	r.Route("/api", func(api chi.Router) {
		api.Route("/games", func(gr chi.Router) {
			gr.Post("/", gh.CreateGame)             // POST /api/games
			gr.Get("/", gh.ListGames)               // GET  /api/games
			gr.Get("/{id}", gh.GetGame)             // GET  /api/games/{id}
			gr.Post("/{id}/throws", gh.PostThrow)   // POST /api/games/{id}/throws
			gr.Post("/{id}/undo", gh.UndoLastThrow) // POST /api/games/{id}/undo
		})
	})

	return r
}

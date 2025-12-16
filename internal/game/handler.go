package game

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// POST /api/games
func (h *Handler) CreateGame(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var req CreateGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	state, err := h.repo.CreateGame(ctx, req)
	if err != nil {
		http.Error(w, "failed to create game: "+err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, state)
}

// GET /api/games/{id}
func (h *Handler) GetGame(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing game id", http.StatusBadRequest)
		return
	}

	state, err := h.repo.GetGame(ctx, id)
	if err != nil {
		http.Error(w, "failed to load game: "+err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// POST /api/games/{id}/throws
func (h *Handler) PostThrow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing game id", http.StatusBadRequest)
		return
	}

	var req CreateThrowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	state, err := h.repo.AddThrow(ctx, id, req)
	if err != nil {
		http.Error(w, "failed to register throw: "+err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// POST /api/games/{id}/undo
func (h *Handler) UndoLastThrow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing game id", http.StatusBadRequest)
		return
	}

	state, err := h.repo.UndoLastThrow(ctx, id)
	if err != nil {
		http.Error(w, "failed to undo throw: "+err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// Helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

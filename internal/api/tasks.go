package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/henok3878/distributed-task-queue/internal/store"
)

func RegisterTasks(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}

		t, err := store.GetTask(r.Context(), d.DB, id)
		if errors.Is(err, pgx.ErrNoRows) {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// stream result as raw JSON
		var result any
		if len(t.ResultJSON) > 0 {
			_ = json.Unmarshal(t.ResultJSON, &result)
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"id":           t.ID,
			"type":         t.Type,
			"queue":        t.Queue,
			"status":       t.Status,
			"attempts":     t.Attempts,
			"max_attempts": t.MaxAttempts,
			"last_error":   t.LastError,
			"result":       result,
			"created_at":   t.CreatedAt,
			"updated_at":   t.UpdatedAt,
		})
	})
}

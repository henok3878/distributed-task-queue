package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/store"
)

type EnqueueRequest struct {
	Type           string          `json:"type"`
	Queue          string          `json:"queue,omitempty"`           // optional override: "default"/"high"
	MaxAttempts    int             `json:"max_attempts,omitempty"`    // optional override
	IdempotencyKey string          `json:"idempotency_key,omitempty"` // optional dedupe
	Payload        json.RawMessage `json:"payload"`                   // required
}
type EnqueueResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Queue  string `json:"queue"`
}

func RegisterEnqueue(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /enqueue", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MiB cap
		defer r.Body.Close()

		var req EnqueueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "invalid json: %v", err); return
		}
		if strings.TrimSpace(req.Type) == "" {
			ErrorJSON(w, http.StatusBadRequest, "type is required"); return
		}
		if len(req.Payload) == 0 || string(req.Payload) == "null" {
			ErrorJSON(w, http.StatusBadRequest, "payload is required"); return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// registry defaults
		active, defQ, defMax, err := store.GetTypeDefaults(ctx, d.DB, req.Type)
		if errors.Is(err, pgx.ErrNoRows) {
			ErrorJSON(w, http.StatusBadRequest, "unknown type %q", req.Type); return
		}
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "db error: %v", err); return
		}
		if !active {
			ErrorJSON(w, http.StatusBadRequest, "type %q is not active", req.Type); return
		}

		// resolve queue/attempts with guardrails
		queue := strings.TrimSpace(req.Queue)
		if queue == "" { queue = defQ }
		if !contains(d.Topology.RoutingKeys, queue) {
			ErrorJSON(w, http.StatusBadRequest, "queue %q not allowed (one of %v)", queue, d.Topology.RoutingKeys); return
		}
		maxAttempts := req.MaxAttempts
		if maxAttempts == 0 { maxAttempts = defMax }
		if maxAttempts < 1 || maxAttempts > 20 {
			ErrorJSON(w, http.StatusBadRequest, "max_attempts out of range (1..20)"); return
		}

		// insert (idempotent on idempotency_key)
		taskID := newID()
		outID, outStatus, outQueue, err := store.UpsertEnqueue(ctx, d.DB, taskID, req.Type, queue, req.Payload, req.IdempotencyKey, maxAttempts)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "insert error: %v", err); return
		}

		// publish minimal persistent message (workers fetch payload by id)
		body, _ := json.Marshal(map[string]string{"id": outID, "type": req.Type})
		pub := amqp.Publishing{ ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body }
		if err := d.RMQ.PublishWithContext(ctx, d.Topology.Namespace+".direct", outQueue, false, false, pub); err != nil {
			ErrorJSON(w, http.StatusServiceUnavailable, "publish failed: %v", err); return
		}

		WriteJSON(w, http.StatusCreated, EnqueueResponse{ID: outID, Status: outStatus, Queue: outQueue})
	})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	dst := make([]byte, hex.EncodedLen(len(b)))
	hex.Encode(dst, b[:])
	return string(dst)
}
func contains(xs []string, want string) bool {
	for _, x := range xs { if x == want { return true } }
	return false
}

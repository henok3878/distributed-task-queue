package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/rmq"
)

type Deps struct {
	DB       *pgxpool.Pool
	RMQ      *amqp.Channel
	Topology rmq.Topology
}

func RegisterHealth(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()

		// DB ready?
		if err := d.DB.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded",
				"db":     "down: " + err.Error(),
			})
			return
		}

		// rmq exchanges exist
		if err := d.RMQ.ExchangeDeclarePassive(
			d.Topology.MainExchange, d.Topology.MainKind, true, false, false, false, nil,
		); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded",
				"rmq":    "main exchange missing: " + err.Error(),
			})
			return
		}
		if err := d.RMQ.ExchangeDeclarePassive(
			d.Topology.DLXExchange, d.Topology.DLXKind, true, false, false, false, nil,
		); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded",
				"rmq":    "dlx exchange missing: " + err.Error(),
			})
			return
		}

		// rmq queues exist
		for _, rk := range d.Topology.RoutingKeys {
			name := d.Topology.FullQueueName(rk)
			if _, err := d.RMQ.QueueDeclarePassive(name, true, false, false, false, nil); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "degraded",
					"rmq":    "queue missing: " + name,
				})
				return
			}
		}
		if _, err := d.RMQ.QueueDeclarePassive(d.Topology.DLQName, true, false, false, false, nil); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded",
				"rmq":    "dlq missing: " + err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

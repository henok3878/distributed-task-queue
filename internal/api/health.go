package api

import (
	"context"
	"net/http"
	"time"
)

func RegisterHealth(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()

		// dp ping
		if err := d.DB.Ping(ctx); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded", "db": "down: " + err.Error(),
			})
			return
		}

		// exchanges
		if err := d.RMQ.ExchangeDeclarePassive(
			d.Topology.MainExchange, "direct", true, false, false, false, nil,
		); err != nil {
			ErrorJSON(w, http.StatusServiceUnavailable, "main exchange missing (%s): %v", d.Topology.MainExchange, err)
			return
		}
		if err := d.RMQ.ExchangeDeclarePassive(
			d.Topology.DLXExchange, "direct", true, false, false, false, nil,
		); err != nil {
			ErrorJSON(w, http.StatusServiceUnavailable, "dlx exchange missing (%s): %v", d.Topology.DLXExchange, err)
			return
		}

		// queues
		for _, rk := range d.Topology.RoutingKeys {
			name := d.Topology.FullQueueName(rk)
			if _, err := d.RMQ.QueueDeclarePassive(name, true, false, false, false, nil); err != nil {
				ErrorJSON(w, http.StatusServiceUnavailable, "queue missing: %s", name)
				return
			}
		}
		if _, err := d.RMQ.QueueDeclarePassive(d.Topology.DLQName, true, false, false, false, nil); err != nil {
			ErrorJSON(w, http.StatusServiceUnavailable, "dlq missing: %v", err)
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/api"
	"github.com/henok3878/distributed-task-queue/internal/config"
	"github.com/henok3878/distributed-task-queue/internal/metrics"
	"github.com/henok3878/distributed-task-queue/internal/rmq"
)

func main() {
	_ = godotenv.Load()

	httpPort, err := config.GetFromEnv("HTTP_PORT")
	if err != nil {
		log.Fatal("config:", err)
	}

	dbDSN, err := config.GetFromEnv("DB_DSN")
	if err != nil {
		log.Fatal("config:", err)
	}

	amqpURL, err := rmq.URLFromEnv()
	if err != nil {
		log.Fatal("config:", err)
	}

	// load topology
	topo, err := rmq.Load()
	if err != nil {
		log.Fatal("config:", err)
	}

	// Postgres pool
	db, err := pgxpool.New(context.Background(), dbDSN)
	if err != nil {
		log.Fatal("pg connect:", err)
	}
	defer db.Close()

	// RabbitMQ connection + channel
	rmqConn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatal("amqp dial:", err)
	}
	defer rmqConn.Close()

	rmqCh, err := rmqConn.Channel()
	if err != nil {
		log.Fatal("amqp channel:", err)
	}
	defer rmqCh.Close()

	// register Prom metrics we defined
	metrics.MustRegisterAll()

	mux := http.NewServeMux()

	// info
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"service": "distributed-task-queue"})
	})

	// /healthz
	api.RegisterHealth(mux, api.Deps{DB: db, RMQ: rmqCh, Topology: topo})
	// /enqueue
	api.RegisterEnqueue(mux, api.Deps{DB: db, RMQ: rmqCh, Topology: topo})
	// /tasks/{id}
	api.RegisterTasks(mux, api.Deps{DB: db, RMQ: rmqCh, Topology: topo})

	// /metrics (Prometheus)
	metrics.Expose(mux, "GET /metrics")

	srv := &http.Server{
		Addr:              httpPort,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	log.Println("api listening on", httpPort)
	log.Fatal(srv.ListenAndServe())
}

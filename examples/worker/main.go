package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/backoff"
	"github.com/henok3878/distributed-task-queue/internal/config"
	"github.com/henok3878/distributed-task-queue/internal/rmq"
	"github.com/henok3878/distributed-task-queue/internal/store"
)

type msgEnvelope struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func main() {
	_ = godotenv.Load()

	// config
	dbDSN, err := config.GetFromEnv("DB_DSN")
	if err != nil {
		log.Fatal("config:", err)
	}
	amqpURL, err := rmq.URLFromEnv()
	if err != nil {
		log.Fatal("config:", err)
	}
	topology, err := rmq.Load()
	if err != nil {
		log.Fatal("config:", err)
	}

	// backoff strategy (env-driven)
	bo := backoff.FromEnv()

	// prefetch (max unacked per consumer)
	prefetch := 32
	if v := os.Getenv("WORKER_PREFETCH"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			prefetch = n
		}
	}

	// connections
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbDSN)
	if err != nil {
		log.Fatal("pg connect:", err)
	}
	defer db.Close()

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatal("amqp dial:", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("amqp channel:", err)
	}
	defer ch.Close()

	if err := ch.Qos(prefetch, 0, false); err != nil {
		log.Fatal("amqp qos:", err)
	}
	log.Printf("worker: prefetch=%d queues=%v", prefetch, topology.RoutingKeys)

	// graceful shutdown
	ctxRun, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	var tags []string

	// one consumer per priority queue
	for _, rk := range topology.RoutingKeys {
		queue := topology.FullQueueName(rk)
		tag := fmt.Sprintf("worker-%s-%d", rk, time.Now().UnixNano())
		tags = append(tags, tag)

		deliveries, err := ch.Consume(queue, tag, false, false, false, false, nil)
		if err != nil {
			log.Fatalf("consume %s: %v", queue, err)
		}

		wg.Add(1)
		go func(q, ctag string, ds <-chan amqp.Delivery) {
			defer wg.Done()
			for {
				select {
				case <-ctxRun.Done():
					return
				case d, ok := <-ds:
					if !ok {
						return
					}

					var env msgEnvelope
					if err := json.Unmarshal(d.Body, &env); err != nil {
						log.Printf("queue=%s parse error: %v body=%q", q, err, string(d.Body))
						_ = d.Ack(false) // drop poison for now
						continue
					}

					start := time.Now()

					// per-message transactional scope
					ctxMsg, cancelMsg := context.WithTimeout(ctxRun, 10*time.Second)
					tx, err := db.Begin(ctxMsg)
					if err != nil {
						cancelMsg()
						log.Printf("begin tx error: %v", err)
						_ = d.Nack(false, true)
						continue
					}

					t, err := store.LockTaskForWork(ctxMsg, tx, env.ID)
					if err != nil {
						_ = tx.Rollback(ctxMsg)
						cancelMsg()
						if err == pgx.ErrNoRows {
							log.Printf("missing task id=%s; ack", env.ID)
							_ = d.Ack(false)
							continue
						}
						log.Printf("lock error id=%s: %v", env.ID, err)
						_ = d.Nack(false, true)
						continue
					}

					// already completed? (idempotent)
					if t.Status == "SUCCEEDED" {
						_ = tx.Commit(ctxMsg)
						cancelMsg()
						_ = d.Ack(false)
						continue
					}

					// attempts guard
					if t.Attempts >= t.MaxAttempts {
						_ = store.MarkFailed(ctxMsg, tx, t.ID, "max attempts exceeded")
						_ = tx.Commit(ctxMsg)
						cancelMsg()
						_ = d.Ack(false)
						continue
					}

					// RUNNING (+attempts)
					if err := store.MarkRunning(ctxMsg, tx, t.ID); err != nil {
						_ = tx.Rollback(ctxMsg)
						cancelMsg()
						_ = d.Nack(false, true)
						continue
					}

					// do work
					resultJSON, handlerErr := handle(ctxMsg, t.Type, t.Payload)

					if handlerErr == nil {
						// write-before-ACK
						if err := store.MarkSucceeded(ctxMsg, tx, t.ID, resultJSON); err != nil {
							_ = tx.Rollback(ctxMsg)
							cancelMsg()
							_ = d.Nack(false, true)
							continue
						}
						if err := tx.Commit(ctxMsg); err != nil {
							cancelMsg()
							_ = d.Nack(false, true)
							continue
						}
						cancelMsg()
						_ = d.Ack(false)
						log.Printf("id=%s ok in %s", env.ID, time.Since(start))
						continue
					}

					// error: retry or final DLQ
					// compute delay using the post increment attempt number
					attemptAfter := t.Attempts + 1
					if attemptAfter < t.MaxAttempts {
						// persist retry state (status back to ENQUEUED, record last_error)
						if err := store.MarkRetry(ctxMsg, tx, t.ID, handlerErr.Error()); err != nil {
							_ = tx.Rollback(ctxMsg)
							cancelMsg()
							_ = d.Nack(false, true)
							continue
						}
						if err := tx.Commit(ctxMsg); err != nil {
							cancelMsg()
							_ = d.Nack(false, true)
							continue
						}
						cancelMsg()

						// compute delay and publish to retry queue with per-message TTL
						delay := bo.NextDelay(attemptAfter)

						rk := strings.TrimPrefix(t.Queue, topology.QueuePrefix+".") // "default" | "high"
						retryQueue := topology.RetryQueueName(rk)

						body, _ := json.Marshal(msgEnvelope{ID: t.ID, Type: t.Type})
						pub := amqp.Publishing{
							ContentType: "application/json",
							Body:        body,
							Expiration:  strconv.FormatInt(delay.Milliseconds(), 10), // TTL in ms
						}
						if err := ch.PublishWithContext(ctxRun, "", retryQueue, false, false, pub); err != nil {
							log.Printf("retry publish %s: %v", retryQueue, err)
							_ = d.Nack(false, true)
							continue
						}

						_ = d.Ack(false)
						log.Printf("id=%s retry in %s -> %s", t.ID, delay, retryQueue)
						continue
					}

					// final failure -> mark FAILED and route to DLQ for inspection
					_ = store.MarkFailed(ctxMsg, tx, t.ID, handlerErr.Error())
					_ = tx.Commit(ctxMsg)
					cancelMsg()

					rk := strings.TrimPrefix(t.Queue, topology.QueuePrefix+".")
					body, _ := json.Marshal(msgEnvelope{ID: t.ID, Type: t.Type})
					_ = ch.PublishWithContext(ctxRun, topology.DLXExchange, rk, false, false, amqp.Publishing{
						ContentType: "application/json",
						Body:        body,
					})

					_ = d.Ack(false)
					log.Printf("id=%s FINAL FAILED: %v", env.ID, handlerErr)
				}
			}
		}(queue, tag, deliveries)
	}

	// wait for shutdown
	<-ctxRun.Done()
	log.Println("worker: shutting down...")
	for _, tag := range tags {
		_ = ch.Cancel(tag, false)
	}
	wg.Wait()
	log.Println("worker: stopped")
}

// example handler
func handle(ctx context.Context, typ string, payload []byte) ([]byte, error) {
	switch typ {
	case "email.send.v1":
		// TODO: actual work goes here
		// simulate the work with some delay
		time.Sleep(100 * time.Millisecond)
		return []byte(`{"ok":true,"provider":"stub"}`), nil
	default:
		return nil, fmt.Errorf("no handler for type %q", typ)
	}
}

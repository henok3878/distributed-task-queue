package main

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/rmq"
)

func main() {
	_ = godotenv.Load()
	if err := ensureTopology(); err != nil {
		log.Fatal(err)
	}
	log.Println("RabbitMQ topology ensured!")
}

func ensureTopology() error {
	amqpURL, err := rmq.URLFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	topo, err := rmq.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("amqp channel: %w", err)
	}
	defer ch.Close()

	// exchanges
	if err := ch.ExchangeDeclare(topo.MainExchange, topo.MainKind, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", topo.MainExchange, err)
	}
	if err := ch.ExchangeDeclare(topo.DLXExchange, topo.DLXKind, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", topo.DLXExchange, err)
	}

	// queues + bindings
	for _, rk := range topo.RoutingKeys {
		q := topo.FullQueueName(rk) // e.g. tasks.default
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %s: %w", q, err)
		}
		if err := ch.QueueBind(q, rk, topo.MainExchange, false, nil); err != nil {
			return fmt.Errorf("bind %s <- %s[%s]: %w", q, topo.MainExchange, rk, err)
		}
	}

	// single DLQ for all priorities + bindings from DLX
	if _, err := ch.QueueDeclare(topo.DLQName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq %s: %w", topo.DLQName, err)
	}
	for _, rk := range topo.RoutingKeys {
		if err := ch.QueueBind(topo.DLQName, rk, topo.DLXExchange, false, nil); err != nil {
			return fmt.Errorf("bind dlq <- %s[%s]: %w", topo.DLXExchange, rk, err)
		}
	}

	// retry queues (one per priority)... per-message TTL is set at publish time
	// these queues dead-letter back to the main exchange with the same routing key
	for _, rk := range topo.RoutingKeys {
		qRetry := topo.RetryQueueName(rk) // e.g. tasks.retry.default
		args := amqp.Table{
			"x-dead-letter-exchange":    topo.MainExchange,
			"x-dead-letter-routing-key": rk,
		}
		if _, err := ch.QueueDeclare(qRetry, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare retry queue %s: %w", qRetry, err)
		}
	}

	return nil
}

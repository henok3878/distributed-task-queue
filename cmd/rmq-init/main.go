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
	err := ensureTopology()
	if err != nil {
		log.Fatal("AMQP Dial failed!", err)
	}
	log.Println("RabbitMQ topology ensured!")
}

func ensureTopology() error {
	amqpUrl, err := rmq.URLFromEnv()
	if err != nil {
		return fmt.Errorf("config", err)
	}
	topo, err := rmq.Load()
	if err != nil {
		return fmt.Errorf("config: ", err)
	}

	conn, err := amqp.Dial(amqpUrl)
	if err != nil {
		return fmt.Errorf("amqp dial (%s): %w", amqpUrl, err)
	}

	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	// durable direct exchanges
	if err := ch.ExchangeDeclare(topo.MainExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", topo.MainExchange, err)
	}
	if err := ch.ExchangeDeclare(topo.DLXExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", topo.DLXExchange, err)
	}

	// queues + bindings
	for _, rk := range topo.RoutingKeys {
		qName := "tasks." + rk
		if _, err := ch.QueueDeclare(qName, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %s: %w", qName, err)
		}
		if err := ch.QueueBind(qName, rk, topo.MainExchange, false, nil); err != nil {
			return fmt.Errorf("bind %s <- %s[%s]: %w", qName, topo.MainExchange, rk, err)
		}
	}

	// dlq + binding
	if _, err := ch.QueueDeclare(topo.DLQName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	for _, rk := range topo.RoutingKeys {
		if err := ch.QueueBind(topo.DLQName, rk, topo.DLXExchange, false, nil); err != nil {
			return fmt.Errorf("bind dlq <- %s[%s]: %w", topo.DLXExchange, rk, err)
		}
	}
	return nil
}

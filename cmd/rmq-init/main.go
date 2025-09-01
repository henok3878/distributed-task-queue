package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/henok3878/distributed-task-queue/internal/config"
	"github.com/henok3878/distributed-task-queue/internal/rmq"
)

const (
	exchangeDirect = "tasks.direct"
	exchangeDLX    = "tasks.dlx"
	queueDLQ       = "tasks.dlq"
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
	queues, err := config.GetFromEnv("QUEUES")

	if err != nil {
		return fmt.Errorf("config: ", err)
	}

	var routingKeys []string
	for _, s := range strings.Split(queues, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			routingKeys = append(routingKeys, s)
		}
	}
	if len(routingKeys) == 0 {
		return fmt.Errorf("QUEUES is set but empty after parsing")
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
	if err := ch.ExchangeDeclare(exchangeDirect, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", exchangeDirect, err)
	}
	if err := ch.ExchangeDeclare(exchangeDLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", exchangeDLX, err)
	}

	// queues + bindings
	for _, rk := range routingKeys {
		qName := "tasks." + rk
		if _, err := ch.QueueDeclare(qName, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %s: %w", qName, err)
		}
		if err := ch.QueueBind(qName, rk, exchangeDirect, false, nil); err != nil {
			return fmt.Errorf("bind %s <- %s[%s]: %w", qName, exchangeDirect, rk, err)
		}
	}

	// dlq + binding
	if _, err := ch.QueueDeclare(queueDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	for _, rk := range routingKeys {
		if err := ch.QueueBind(queueDLQ, rk, exchangeDLX, false, nil); err != nil {
			return fmt.Errorf("bind dlq <- %s[%s]: %w", exchangeDLX, rk, err)
		}
	}
	return nil
}

package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	amqp "github.com/rabbitmq/amqp091-go"
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
}

func ensureTopology() error {
	url := rmqURL()
	queues := env("QUEUES")

	var routingKeys []string
	for _, s := range strings.Split(queues, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			routingKeys = append(routingKeys, s)
		}
	}
	if len(routingKeys) == 0 {
		log.Fatal("QUEUES is set but empty after parsing")
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("amqp dial (%s): %w", url, err)
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

	log.Println("RabbitMQ topology ensured!")
	return nil

}

func rmqURL() string {
	v := os.Getenv("RMQ_URL")
	if v != "" {
		return v
	}

	user := env("RMQ_USER")
	pass := env("RMQ_PASS")
	host := env("RMQ_HOST")
	port := env("RMQ_PORT")
	vhost := env("RMQ_VHOST")
	if !strings.HasPrefix(vhost, "/") {
		vhost = "/" + vhost
	}
	return fmt.Sprintf("amqp://%s:%s@%s:%s%s", user, pass, host, port, vhost)
}

func env(k string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		log.Fatalf("required env %s is not set", k)
	}
	return v
}

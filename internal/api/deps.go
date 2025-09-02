package api

import (
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/henok3878/distributed-task-queue/internal/rmq"
)

type Deps struct {
	DB       *pgxpool.Pool
	RMQ      *amqp.Channel
	Topology rmq.Topology
}
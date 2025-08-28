# ---- config ----
COMPOSE = docker compose --env-file .env -f deploy/docker-compose.yml

# ---- targets ----
.PHONY: up down destroy init dev-reset logs ps config rmq-users

# start services (RabbitMQ + Postgres)
up:
	$(COMPOSE) up -d rabbitmq postgres

# stop services (keep data volumes)
down:
	$(COMPOSE) down

# stop services AND delete data volumes (fresh slate)
destroy:
	$(COMPOSE) down -v

# run Go initializer (idempotent RMQ declarations)
init:
	go run cmd/rmq-init/main.go

# fresh dev cycle: wipe -> start -> declare topology
dev-reset: destroy up init

# tail container logs
logs:
	$(COMPOSE) logs -f --tail=200

# show running containers for this compose
ps:
	$(COMPOSE) ps

# show fully rendered compose config (to verify .env values)
config:
	$(COMPOSE) config

# list RabbitMQ users (inside the container)
rmq-users:
	docker exec -it dq-rabbitmq rabbitmqctl list_users


# ---- config ----
SHELL := /bin/bash
COMPOSE = docker compose --env-file .env -f deploy/docker-compose.yml

# ---- targets ----
.PHONY: up down destroy init dev-reset logs ps config rmq-users db-apply db-seed-type db-shell db-wait api

# start services (RabbitMQ + Postgres)
up:
	$(COMPOSE) up -d rabbitmq postgres

# stop services (keep data volumes)
down:
	$(COMPOSE) down

# stop services AND delete data volumes (fresh slate)
destroy:
	$(COMPOSE) down -v

# ensure Postgres is accepting connections (uses values from .env)
db-wait:
	@bash -c 'set -euo pipefail; source .env; \
	  echo "waiting for Postgres ($$PG_USER@$$PG_DATABASE) ..."; \
	  until docker exec dq-postgres pg_isready -U "$$PG_USER" -d "$$PG_DATABASE" >/dev/null 2>&1; do sleep 1; done; \
	  echo "Postgres is ready."'

# apply type_registry.sql and schema.sql inside the container
db-apply: db-wait
	@bash -c 'set -euo pipefail; source .env; \
	  echo "applying db/type_registry.sql ..."; \
	  docker exec -i dq-postgres psql -v ON_ERROR_STOP=1 -U "$$PG_USER" -d "$$PG_DATABASE" < db/type_registry.sql; \
	  echo "applying db/schema.sql ..."; \
	  docker exec -i dq-postgres psql -v ON_ERROR_STOP=1 -U "$$PG_USER" -d "$$PG_DATABASE" < db/schema.sql; \
	  echo "DB schema applied."'

# optional: seed one sample task type (maps to your queues)
db-seed-type: db-wait
	@bash -c 'set -euo pipefail; source .env; \
	  echo "seeding task_type(email.send.v1 → high, 5 attempts) ..."; \
	  docker exec -i dq-postgres psql -v ON_ERROR_STOP=1 -U "$$PG_USER" -d "$$PG_DATABASE" \
	    -c "INSERT INTO task_type (type,active,default_queue,default_max_attempts) VALUES ('\''email.send.v1'\'', true, '\''high'\'', 5) ON CONFLICT (type) DO NOTHING;"; \
	  echo "seed done."'

# run Go initializer (idempotent RMQ declarations from env topology)
init:
	go run cmd/rmq-init/main.go

# fresh dev cycle: wipe -> start -> db -> rmq topology
dev-reset: destroy up db-apply init

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

# open interactive psql inside the container
db-shell: db-wait
	@bash -c 'set -euo pipefail; source .env; \
	  docker exec -it dq-postgres psql -U "$$PG_USER" -d "$$PG_DATABASE"'

# run API 
api:
	go run cmd/api/main.go

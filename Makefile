# Kenyan Civic Social Platform — developer commands.
# Run `make help` to list targets. Settings can be overridden in .env (see .env.example).

-include .env

POSTGRES_USER      ?= civic
POSTGRES_PASSWORD  ?= civic
POSTGRES_DB        ?= civic
POSTGRES_PORT      ?= 5433
POSTGRES_TEST_PORT ?= 55433
export POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB POSTGRES_PORT POSTGRES_TEST_PORT

COMPOSE := docker compose

# URLs as seen from inside the compose network (used by the migrate container)
DB_URL      := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/$(POSTGRES_DB)?sslmode=disable
TEST_DB_URL := postgres://civic:civic@postgres-test:5432/civic_test?sslmode=disable

# URLs as seen from the host (for Go code, IDEs, psql clients)
HOST_DB_URL      := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
HOST_TEST_DB_URL := postgres://civic:civic@localhost:$(POSTGRES_TEST_PORT)/civic_test?sslmode=disable

MIGRATE      := $(COMPOSE) --profile tools run --rm migrate -path /migrations
MIGRATE_TEST := $(MIGRATE) -database "$(TEST_DB_URL)"
MIGRATE_DEV  := $(MIGRATE) -database "$(DB_URL)"

N ?= 1

.DEFAULT_GOAL := help
.PHONY: help db-up db-down db-reset db-logs db-ps db-psql db-url \
        migrate-up migrate-down migrate-down-all migrate-version migrate-force migrate-create \
        test-db test-db-up test-db-run test-db-down test-db-psql

help: ## List available commands
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## ---- Development database -------------------------------------------------

db-up: ## Start the dev Postgres and wait until healthy
	$(COMPOSE) up -d --wait postgres

db-down: ## Stop the dev Postgres (data is kept)
	$(COMPOSE) stop postgres

db-reset: ## DESTRUCTIVE: delete dev data, recreate the DB and apply all migrations
	$(COMPOSE) rm -sf postgres
	docker volume rm -f civic_pgdata
	$(MAKE) db-up migrate-up

db-logs: ## Tail dev Postgres logs
	$(COMPOSE) logs -f postgres

db-ps: ## Show project containers
	$(COMPOSE) ps -a

db-psql: ## Open psql in the dev database
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

db-url: ## Print connection URLs for the host
	@echo "dev:  $(HOST_DB_URL)"
	@echo "test: $(HOST_TEST_DB_URL)"

## ---- Migrations (dev database) --------------------------------------------

migrate-up: ## Apply all pending migrations
	$(MIGRATE_DEV) up

migrate-down: ## Roll back the last N migrations (N=1)
	$(MIGRATE_DEV) down $(N)

migrate-down-all: ## Roll back every migration
	$(MIGRATE_DEV) down -all

migrate-version: ## Show the current migration version
	$(MIGRATE_DEV) version

migrate-force: ## Force the version after a failed migration (V=<version>)
	@test -n "$(V)" || (echo "usage: make migrate-force V=<version>" && exit 1)
	$(MIGRATE_DEV) force $(V)

migrate-create: ## Create a new migration pair (NAME=<snake_case_name>)
	@test -n "$(NAME)" || (echo "usage: make migrate-create NAME=<snake_case_name>" && exit 1)
	$(COMPOSE) --profile tools run --rm --user $$(id -u):$$(id -g) migrate create -ext sql -dir /migrations -seq $(NAME)

## ---- Database tests (throwaway database) ----------------------------------

test-db: ## Fresh test DB: migrate up, run test/db/*_test.sql, down/up round-trip, tear down
	@$(MAKE) test-db-up
	@status=0; $(MAKE) test-db-run || status=$$?; $(MAKE) test-db-down; exit $$status

test-db-up: ## Start the throwaway test Postgres
	$(COMPOSE) --profile test up -d --wait postgres-test

test-db-run: ## Run migrations and SQL tests against a running test DB
	$(MIGRATE_TEST) up
	@for f in $$(ls test/db/*_test.sql | sort); do \
		echo "== $$f"; \
		$(COMPOSE) --profile test exec -T postgres-test psql -q -o /dev/null -v ON_ERROR_STOP=1 -U civic -d civic_test < $$f || exit 1; \
	done
	$(MIGRATE_TEST) down -all
	$(MIGRATE_TEST) up

test-db-down: ## Remove the test Postgres (its data is in-memory)
	$(COMPOSE) --profile test rm -sf postgres-test

test-db-psql: ## Open psql in the running test database
	$(COMPOSE) --profile test exec postgres-test psql -U civic -d civic_test

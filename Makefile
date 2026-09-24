# Kenyan Civic Social Platform — developer commands.
# Run `make help` to list targets. Settings can be overridden in .env (see .env.example).

-include .env

POSTGRES_USER      ?= civic
POSTGRES_PASSWORD  ?= civic
POSTGRES_DB        ?= civic
POSTGRES_PORT      ?= 5433
POSTGRES_TEST_PORT ?= 55433
KAFKA_PORT         ?= 19092
REDIS_PORT         ?= 16379
export POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB POSTGRES_PORT POSTGRES_TEST_PORT KAFKA_PORT REDIS_PORT

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
        test-db test-db-up test-db-run test-db-down test-db-psql \
        db-seed admin-grant run-api run-worker kafka-up redis-up test-api build test test-integration test-all sqlc-generate sqlc-check fmt vet

# Development-only secrets for run-api. Production uses KMS/Vault (T-X.4).
DEV_JWT_SIGNING_KEY      ?= dev-only-jwt-signing-key-change-me-0123456789
DEV_NATIONAL_ID_PEPPER   ?= dev-only-national-id-pepper-change-me-0123
DEV_RATE_LIMIT_KEY       ?= dev-only-rate-limit-key-change-me-0123456789
DEV_PII_ENCRYPTION_KEY   ?= dev-only-pii-encryption-key-change-me-012345
DEV_FEED_SIGNING_KEY     ?= dev-only-feed-signing-key-change-me-0123456
SQLC := docker run --rm -u $$(id -u):$$(id -g) -v "$(CURDIR)":/src -w /src sqlc/sqlc:1.27.0

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
	@$(MAKE) test-db-down test-db-up
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

## ---- Go -------------------------------------------------------------------

db-seed: ## Load reference data (IEBC counties, constituencies, wards) into the dev database
	go run ./cmd/seed -database "$(HOST_DB_URL)"

admin-grant: ## Grant a platform role in the dev database: make admin-grant USER_ID=<public id> ROLE=sysadmin
	go run ./cmd/admin -database "$(HOST_DB_URL)" -user "$(USER_ID)" -role "$(or $(ROLE),sysadmin)"

run-api: db-up migrate-up db-seed redis-up ## Run the API against the dev database (dev-only secrets)
	DATABASE_URL="$(HOST_DB_URL)" \
	REDIS_URL="redis://localhost:$(REDIS_PORT)/0" \
	RATE_LIMIT_KEY="$(DEV_RATE_LIMIT_KEY)" \
	FEED_SIGNING_KEY="$(DEV_FEED_SIGNING_KEY)" \
	PII_ENCRYPTION_KEY="$(DEV_PII_ENCRYPTION_KEY)" \
	JWT_SIGNING_KEY="$(DEV_JWT_SIGNING_KEY)" \
	NATIONAL_ID_PEPPER="$(DEV_NATIONAL_ID_PEPPER)" \
	go run ./cmd/api

redis-up: ## Start Redis (cache, rate limits) on localhost:$(REDIS_PORT)
	$(COMPOSE) up -d --wait redis

kafka-up: ## Start the dev event bus (Redpanda, Kafka API on localhost:$(KAFKA_PORT))
	$(COMPOSE) up -d --wait redpanda

run-worker: db-up migrate-up kafka-up ## Run the outbox relay (publishes events to Kafka)
	DATABASE_URL="$(HOST_DB_URL)" KAFKA_BROKERS="localhost:$(KAFKA_PORT)" go run ./cmd/worker

test-api: ## Run the Postman collection with Newman against a running API (make run-api)
	docker run --rm -v "$(CURDIR)/api/postman":/etc/newman postman/newman:6-alpine \
		run civic-platform.postman_collection.json --env-var baseUrl=http://host.docker.internal:8090

build: ## Build all binaries into bin/
	go build -trimpath -o bin/ ./cmd/...

fmt: ## Format Go code
	gofmt -w cmd internal pkg

vet: ## Run go vet
	go vet ./...

test: ## Unit tests (integration tests skip without a database)
	go test -race -count=1 ./...

test-integration: ## Go tests against a fresh, migrated test database and the event bus
	@$(MAKE) test-db-down test-db-up kafka-up redis-up
	@status=0; \
	$(MIGRATE_TEST) up && TEST_DATABASE_URL="$(HOST_TEST_DB_URL)" KAFKA_BROKERS="localhost:$(KAFKA_PORT)" \
		TEST_REDIS_URL="redis://localhost:$(REDIS_PORT)/15" \
		go test -race -count=1 -p 1 ./... || status=$$?; \
	$(MAKE) test-db-down; exit $$status

test-all: test-db test-integration ## SQL schema tests, then Go tests against a real database

sqlc-generate: ## Regenerate Go code from db/queries
	$(SQLC) generate

sqlc-check: ## Fail if generated sqlc code is out of date
	$(SQLC) diff

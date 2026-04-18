.PHONY: help run build migrate migrate-down migrate-reset migrate-status migrate-create tidy test vet docker-up docker-down web-install web-dev web-build web-check

BINARY_NAME := goxero
BUILD_DIR   := bin
COMPOSE     := docker compose -f compose.dev.yml

help:
	@echo "Available commands:"
	@echo "  make docker-up       - start Postgres via compose.dev.yml"
	@echo "  make docker-down     - stop the compose.dev.yml stack"
	@echo "  make migrate         - run all pending migrations (goose up)"
	@echo "  make migrate-down    - revert the last migration"
	@echo "  make migrate-reset   - roll back all migrations"
	@echo "  make migrate-status  - show migration status"
	@echo "  make migrate-create NAME=xxx - create a new migration"
	@echo "  make run             - start the HTTP server"
	@echo "  make build           - compile binaries to ./bin"
	@echo "  make tidy            - go mod tidy"
	@echo "  make test            - go test ./..."
	@echo "  make vet             - go vet ./..."
	@echo "  make web-install     - bun install in web/"
	@echo "  make web-dev         - run SvelteKit dev server"
	@echo "  make web-build       - build SvelteKit production bundle"
	@echo "  make web-check       - run svelte-check type & a11y checks"

docker-up:
	$(COMPOSE) up -d postgres

docker-down:
	$(COMPOSE) down

migrate:
	./scripts/migrate up

migrate-down:
	./scripts/migrate down

migrate-reset:
	./scripts/migrate reset

migrate-status:
	./scripts/migrate status

migrate-create:
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-create NAME=add_foo"; exit 1; fi
	./scripts/migrate create $(NAME) sql

run:
	go run ./cmd/server

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server

tidy:
	go mod tidy

test:
	go test ./...

vet:
	go vet ./...

web-install:
	cd web && bun install

web-dev:
	cd web && bun run dev

web-build:
	cd web && bun run build

web-check:
	cd web && bun run check

SHELL := /bin/bash
export GOCACHE := $(CURDIR)/.local/go-cache
export GOMODCACHE := $(CURDIR)/.local/go-mod
DC = docker compose --env-file .local/.env
.PHONY: setup infra-up test test-integration up down logs migrate images seed smoke lint check
setup:
	go run ./cmd/devsetup
infra-up: setup
	$(DC) up -d postgres kafka kafka-init storage mailpit
test:
	go test -race ./...
lint:
	test -z "$$(gofmt -l cmd internal services tests)"
	go vet ./...
images:
	$(DC) build
migrate:
	$(DC) run --rm migrate
up: setup
	$(DC) up -d --build
logs:
	$(DC) logs -f --tail=50
down:
	$(DC) down

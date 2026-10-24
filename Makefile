SHELL := /bin/bash
export GOCACHE := $(CURDIR)/.local/go-cache
export GOMODCACHE := $(CURDIR)/.local/go-mod
DC = docker compose --env-file .local/.env
.PHONY: setup infra-up test test-integration up down logs migrate images seed smoke lint check test-browser test-resilience observability-up observability-check tools helm-check contracts-check kind-up kind-smoke
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
	$(DC) --parallel 2 up -d --build --wait
logs:
	$(DC) logs -f --tail=50
down:
	$(DC) down
seed:
	go run ./cmd/seed
smoke:
	go test -tags=smoke ./tests/smoke -v -count=1
test-integration:
	go test -tags=integration ./tests/integration -v -count=1
test-browser:
	cd tests/browser && npm ci && npx playwright test
test-resilience:
	go test -tags=resilience ./tests/resilience -v -count=1
observability-up: setup
	$(DC) -f compose.yaml -f compose.observability.yaml up -d
observability-check:
	node tests/browser/check-observability.mjs
tools:
	bash scripts/install-tools.sh
helm-check:
	bash scripts/check-helm.sh
contracts-check:
	node tests/browser/check-contracts.mjs
kind-up: images
	bash scripts/kind-up.sh
kind-smoke:
	BASE_URL=http://127.0.0.1:18080 MAILPIT_URL=http://127.0.0.1:18025 go test -tags=smoke ./tests/smoke -v -count=1
check: lint test test-integration smoke

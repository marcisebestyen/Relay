BACKEND := relay-backend
INFRA := relay-infrastructure
COMPOSE := docker compose -f $(INFRA)/docker-compose.yml
BIN := $(CURDIR)/bin/relay
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test lint infra-up infra-down infra-reset

build:
	cd $(BACKEND) && go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/relay

test:
	cd $(BACKEND) && go test ./...

lint:
	cd $(BACKEND) && golangci-lint run ./...

infra-up:
	$(COMPOSE) up -d --wait

infra-down:
	$(COMPOSE) down

# DESTRUCTIVE: deletes the Postgres volume and all local data.
infra-reset:
	$(COMPOSE) down --volumes

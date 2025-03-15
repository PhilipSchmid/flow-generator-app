GO := go
GOLANGCI_LINT := golangci-lint
DOCKER := docker
SERVER_IMAGE := ghcr.io/philipschmid/echo-server
CLIENT_IMAGE := ghcr.io/philipschmid/flow-generator
VERSION := latest

.PHONY: all build test lint clean docker-build

all: build

build:
	$(GO) build -o echo-server ./cmd/server
	$(GO) build -o flow-generator ./cmd/client

test:
	$(GO) test ./...

lint:
	$(GOLANGCI_LINT) run

clean:
	rm -f echo-server flow-generator

docker-build:
	$(DOCKER) build -t $(SERVER_IMAGE):$(VERSION) -f Dockerfile.server .
	$(DOCKER) build -t $(CLIENT_IMAGE):$(VERSION) -f Dockerfile.client .
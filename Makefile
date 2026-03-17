.PHONY: all build tidy test lint run-chat run-server clean

BINARY := axon
CMD     := ./cmd/axon
GOFLAGS := -ldflags="-s -w"

all: tidy build

## tidy: download and verify dependencies
tidy:
	go mod tidy

## build: compile the axon binary
build:
	go build $(GOFLAGS) -o $(BINARY) $(CMD)

## test: run tests with race detector
test:
	go test -race -count=1 ./...

## lint: run golangci-lint (install: https://golangci-lint.run)
lint:
	golangci-lint run ./...

## run-chat: run in interactive chat mode
run-chat: build
	./$(BINARY) chat

## run-server: run HTTP server on :8080
run-server: build
	./$(BINARY) server

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -f axon.db

## help: print available targets
help:
	@grep -E '^##' Makefile | sed 's/## //'

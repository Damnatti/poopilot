GO := $(shell which go || echo "/usr/local/Cellar/go/1.26.1/bin/go")

.PHONY: build test test-cover lint run clean

build:
	$(GO) build -o bin/poopilot ./cmd/poopilot

test:
	$(GO) test ./internal/... -v -race -count=1

test-cover:
	$(GO) test ./internal/... -coverprofile=coverage.out -race -count=1
	$(GO) tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

run:
	$(GO) run ./cmd/poopilot run -- $(CMD)

clean:
	rm -rf bin/ coverage.out coverage.html

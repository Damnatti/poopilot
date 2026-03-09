GO := $(shell which go || echo "/usr/local/Cellar/go/1.26.1/bin/go")
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/denismelnikov/poopilot/internal/cli.Version=$(VERSION)

.PHONY: build install test test-cover lint run clean

build:
	$(GO) build -ldflags '$(LDFLAGS)' -o bin/poopilot ./cmd/poopilot

install:
	$(GO) install -ldflags '$(LDFLAGS)' ./cmd/poopilot

test:
	$(GO) test ./internal/... -v -race -count=1

test-cover:
	$(GO) test ./internal/... -coverprofile=coverage.out -race -count=1
	$(GO) tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

run:
	$(GO) run -ldflags '$(LDFLAGS)' ./cmd/poopilot run -- $(CMD)

clean:
	rm -rf bin/ coverage.out coverage.html

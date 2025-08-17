.PHONY: build run lint clean test help

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the server binary
	go build -o double-agent main.go

run: ## Run the server
	go run main.go

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format code with gofmt
	go fmt ./...

test: ## Run tests
	go test -v ./...

test-coverage: ## Run tests with coverage
	go test -v -cover ./...

clean: ## Clean build artifacts
	rm -f double-agent
	rm -f /tmp/double-agent.sock

all: clean build test ## Clean, build, and test

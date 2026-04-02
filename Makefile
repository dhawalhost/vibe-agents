.PHONY: build test lint run clean install help

BINARY_NAME=vibe
BUILD_DIR=./bin
CMD_DIR=./cmd/vibe

# Go build flags
LDFLAGS=-ldflags "-X main.Version=1.0.0"

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

install: ## Install the binary to GOPATH/bin
	go install $(LDFLAGS) $(CMD_DIR)
	@echo "Installed $(BINARY_NAME) to $$(go env GOPATH)/bin"

test: ## Run all tests
	go test ./... -v -count=1

test-coverage: ## Run tests with coverage
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint: ## Run linter (requires golangci-lint)
	@which golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

fmt: ## Format code
	gofmt -w .
	goimports -w . 2>/dev/null || true

vet: ## Run go vet
	go vet ./...

run: build ## Build and run with a sample prompt
	$(BUILD_DIR)/$(BINARY_NAME) generate "Build a REST API with user authentication and PostgreSQL"

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

.DEFAULT_GOAL := help

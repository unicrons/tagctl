.PHONY: build test lint clean run install help fmt vet hooks check-secrets check-fmt tools

# Variables
BINARY_NAME=tagctl
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet
GOFMT=$(GOCMD) fmt

# Default target
all: fmt vet test build

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

## build: Build the binary
build:
	$(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/tagctl

## test: Run tests
test:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

## test-short: Run tests without race detector
test-short:
	$(GOTEST) -v ./...

## coverage: Show test coverage report
coverage: test
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## lint: Run golangci-lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, running go vet only"; \
		$(GOVET) ./...; \
	fi

## vet: Run go vet
vet:
	$(GOVET) ./...

## fmt: Format code
fmt:
	$(GOFMT) ./...

## clean: Remove build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

## deps: Download and tidy dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

## install: Install binary to GOPATH/bin (or ~/go/bin if GOPATH not set)
install: build
	@mkdir -p $(or $(GOPATH),$(HOME)/go)/bin
	cp bin/$(BINARY_NAME) $(or $(GOPATH),$(HOME)/go)/bin/

## run: Build and run
run: build
	./bin/$(BINARY_NAME)

## scan: Run tagctl scan
scan: build
	./bin/$(BINARY_NAME) scan

## plan: Run tagctl plan
plan: build
	./bin/$(BINARY_NAME) plan

## hooks: Install git hooks
hooks:
	@echo "Installing git hooks..."
	@git config core.hooksPath .githooks
	@echo "Git hooks installed. Pre-commit checks are now enabled."

## hooks-uninstall: Uninstall git hooks
hooks-uninstall:
	@echo "Uninstalling git hooks..."
	@git config --unset core.hooksPath
	@echo "Git hooks uninstalled."

## check: Run all pre-commit checks manually
check: check-secrets check-fmt vet build test-short
	@echo ""
	@echo "All checks passed!"

## check-secrets: Check for secrets in staged files
check-secrets:
	@echo "Checking for secrets..."
	@./.githooks/check-secrets.sh || true

## iam-templates: Re-render permissions/aws/*.yaml from the JSON policies
iam-templates:
	@python3 scripts/render-iam-templates.py

## check-fmt: Check if code is formatted
check-fmt:
	@echo "Checking Go formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt' to fix formatting:" && gofmt -l . && exit 1)

## tools: Install development tools (trufflehog, golangci-lint)
tools:
	@echo "Installing development tools..."
	@if ! command -v trufflehog >/dev/null 2>&1; then \
		echo "Installing trufflehog..."; \
		brew install trufflehog 2>/dev/null || \
		curl -sSfL https://raw.githubusercontent.com/trufflesecurity/trufflehog/v3.97.4/scripts/install.sh | sh -s -- -b /usr/local/bin v3.97.4; \
	else \
		echo "trufflehog already installed"; \
	fi
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		brew install golangci-lint 2>/dev/null || \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2; \
	else \
		echo "golangci-lint already installed"; \
	fi
	@echo "Done! Run 'make hooks' to enable pre-commit checks."

## setup: Complete development setup (tools + hooks)
setup: tools hooks
	@echo ""
	@echo "Development environment ready!"

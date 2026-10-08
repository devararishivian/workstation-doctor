BINARY := workstation-doctor
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
TEST_PKG ?= ./...
TEST_RUN ?= .

.PHONY: build test test-platform vet lint lint-fix fmt vuln run clean install help

help: ## show targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-14s %s\n", $$1, $$2}'

build: ## build binary (version from git)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test: ## run tests with data race detector
	go test -race -run '$(TEST_RUN)' $(TEST_PKG)

test-platform: ## run platform-specific tests (requires EXPECT_OS=darwin|linux)
	@if [ -z "$(EXPECT_OS)" ]; then \
		echo "EXPECT_OS is required (e.g. make test-platform EXPECT_OS=darwin)"; exit 1; \
	fi
	@if [ "$$(go env GOOS)" != "$(EXPECT_OS)" ]; then \
		echo "Host OS $$(go env GOOS) does not match expected $(EXPECT_OS)"; exit 1; \
	fi
	go test -race ./...

vet: ## run go vet
	go vet ./...

lint: ## run linters (must report 0 issues)
	golangci-lint run ./...

lint-fix: ## auto-fix lint issues where possible
	golangci-lint run --fix ./...

fmt: ## format code
	golangci-lint fmt ./...

vuln: ## scan dependencies for known vulnerabilities
	govulncheck ./...

run: build ## build and run (override with ARGS="check --db /tmp/x.db")
	./$(BINARY) $(ARGS)

clean: ## remove built binary and local database files
	rm -f $(BINARY) *.db

install: ## install binary to GOPATH/bin
	go install -ldflags "$(LDFLAGS)" .

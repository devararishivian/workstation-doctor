BINARY := workstation-doctor
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet lint lint-fix fmt vuln run clean install help

help: ## show targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

build: ## build binary (version from git)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test: ## run tests
	go test ./...

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

clean: ## remove built binary
	rm -f $(BINARY)

install: ## install binary to GOPATH/bin
	go install -ldflags "$(LDFLAGS)" .

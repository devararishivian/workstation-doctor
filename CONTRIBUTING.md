# Contributing to workstation-doctor

Thank you for your interest in contributing to workstation-doctor.

## Ground Rules

1. Keep audits read-only. Never add code that modifies files, updates caches, or fetches remote repositories during health inspection.
2. Every maintenance action requires human confirmation. Never run commands in the background without explicit approval.
3. Protect secrets. Never inspect or display API keys, credentials, or token values from configuration files.
4. Pass all quality gates before opening a pull request.

## Development Setup

### Requirements

- Go 1.22 or newer (Go 1.24+ recommended)
- `golangci-lint` (v1.64+)
- `govulncheck` (`go install golang.org/x/vuln/cmd/govulncheck@latest`)

### Build and Test

Clone your fork and run the verification suite:

```sh
git clone https://github.com/<your-username>/workstation-doctor.git
cd workstation-doctor

# Build the binary
make build

# Run formatting, tests, linter, and vulnerability checks
make vet test lint vuln
```

To run platform-specific tests on your current operating system:

```sh
# On macOS
make test-platform EXPECT_OS=darwin

# On Linux
make test-platform EXPECT_OS=linux
```

## Pull Request Process

1. Create a feature branch from `develop`.
2. Write tests for new functionality or bug fixes.
3. Make sure that `make vet test lint vuln` passes with zero issues and zero vulnerabilities.
4. Open a pull request against the `develop` branch with a clear description of the change.

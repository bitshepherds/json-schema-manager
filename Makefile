.PHONY: all build run clean test test-race test-cover test-race-coverage cover-html lint fmt snapshot release-check setup setup-ci setup-coverage setup-refresh

all: build

# Build the application
build:
	@go run scripts/build/main.go

# Run GoReleaser snapshot
snapshot:
	@go run scripts/snapshot/main.go

# Run the application
run:
	@go run cmd/jsm/main.go

# Clean build artifacts
clean:
	@go run scripts/clean/main.go
	@go clean

# Run tests
test:
	@go run scripts/tester/main.go ./... -v

# Run tests with race detection
test-race:
	@go run scripts/tester/main.go -race -count=1 ./... -v

# Run tests with coverage and show summary. Note that for CI and pre-commit, use test-race-coverage instead
# As this amalgamates the project-wide coverage across packages.
test-cover:
	@go run scripts/tester/main.go --summary -count=1 -race ./...

# The quality gate for CI and pre-commit - combines test-race and project-wide test coverage
test-race-coverage:
	@go run scripts/tester/main.go --test-race-coverage -count=1 -race ./...

# View coverage in browser
cover-html:
	@go run scripts/tester/main.go --browser -count=1 -race ./...

# Generate coverage badge
test-badge:
	@go run scripts/tester/main.go --badge -count=1 -race ./...

# Run linter
lint:
	@go run scripts/lint/main.go

# Format code with gofumpt
fmt:
	@go run scripts/fmt/main.go

# Run GoReleaser configuration check
release-check:
	@go run scripts/release_check/main.go

# Setup development environment
setup:
	@go run scripts/setup/main.go

# Setup for CI - skips lefthook installation
setup-ci:
	@go run scripts/setup/main.go --workflow ci

# Setup for coverage workflow - minimal tools
setup-coverage:
	@go run scripts/setup/main.go --workflow coverage

# Refresh development environment - force reinstall all tools to latest versions
setup-refresh:
	@go run scripts/setup/main.go --force

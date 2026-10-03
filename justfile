set shell := ["bash", "-uc"]

# Default recipe - show available commands
default:
    @just --list

# Format all Go code
fmt:
    gofmt -s -w .

# Check if code is formatted correctly
check-formatted:
    test -z "$(gofmt -s -l .)" || (gofmt -s -l . && exit 1)

# Run linters
lint:
    golangci-lint run --timeout=2m ./...

# Run linters with auto-fix
lint-fix:
    golangci-lint run --fix --timeout=2m ./...

# Run all tests
test:
    go test ./...

# Run tests with race detector
test-race:
    go test -race ./...

# Run tests with coverage
test-coverage:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# Fuzz the SMF reader
fuzz time="30s":
    go test -run='^$' -fuzz=FuzzRead -fuzztime={{ time }} ./smf

# Ensure go.mod is tidy
check-tidy:
    go mod tidy
    git diff --exit-code go.mod

# Run all checks
ci: check-formatted test-race lint check-tidy

# Remove coverage output
clean:
    rm -f coverage.out coverage.html

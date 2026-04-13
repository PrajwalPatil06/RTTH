# Makefile — RTTH developer targets
# Usage: make <target>

GO      := go
GOFLAGS := -count=1 -timeout=120s
RACE    := -race

.PHONY: all lint vet test test-unit test-integration test-race coverage \
        build build-all clean help

# ── Default ───────────────────────────────────────────────────────────────────
all: lint test build

# ── Static analysis ───────────────────────────────────────────────────────────
vet:
	$(GO) vet ./...

lint: vet
	@which staticcheck > /dev/null 2>&1 \
		&& staticcheck ./... \
		|| echo "staticcheck not installed — run: go install honnef.co/go/tools/cmd/staticcheck@latest"

# ── Tests ─────────────────────────────────────────────────────────────────────

## Run all unit tests (no -race, fast feedback loop).
test-unit:
	$(GO) test $(GOFLAGS) ./internal/...

## Run unit tests with the race detector.
test-race:
	$(GO) test $(RACE) $(GOFLAGS) ./internal/...

## Run integration tests only.
test-integration:
	$(GO) test $(RACE) $(GOFLAGS) -v ./test/integration/...

## Run everything: unit + integration, race detector enabled.
test: test-race test-integration

# ── Coverage ──────────────────────────────────────────────────────────────────

## Generate coverage report and open it in the browser.
coverage:
	$(GO) test $(RACE) $(GOFLAGS) \
		-coverprofile=coverage.out \
		-covermode=atomic \
		./internal/...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Print per-function coverage to stdout.
coverage-func:
	$(GO) test $(GOFLAGS) \
		-coverprofile=coverage.out \
		-covermode=atomic \
		./internal/...
	$(GO) tool cover -func=coverage.out

# ── Build ─────────────────────────────────────────────────────────────────────

## Build the RAFT server binary.
build:
	$(GO) build -o bin/server .

## Build all binaries (server + channel + client).
build-all:
	$(GO) build -o bin/server       .
	$(GO) build -o bin/channel      ./cmd/channel/
	$(GO) build -o bin/client       ./cmd/client/

# ── Run (development) ─────────────────────────────────────────────────────────

## Start the channel proxy on port 9000.
run-channel:
	$(GO) run ./cmd/channel/

## Start three server nodes in the background (requires tmux or separate terminals).
run-cluster: build-all
	@echo "Starting 3-node cluster..."
	@mkdir -p data/node1 data/node2 data/node3
	./bin/server 1 8081 300 ./data/node1 &
	./bin/server 2 8082 300 ./data/node2 &
	./bin/server 3 8083 300 ./data/node3 &
	@echo "Nodes started. Use 'make stop-cluster' to stop."

## Kill all running server instances.
stop-cluster:
	@pkill -f 'bin/server' 2>/dev/null || true
	@pkill -f 'bin/channel' 2>/dev/null || true

# ── Utilities ─────────────────────────────────────────────────────────────────

## Remove build artifacts and coverage files.
clean:
	rm -rf bin/ coverage.out coverage.html data/

## Show this help.
help:
	@echo "Available targets:"
	@grep -E '^##' Makefile | sed 's/## /  /'
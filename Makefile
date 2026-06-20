# QueryLynx — Phase 0 Makefile

EXECUTOR_ADDR ?= :8081
EXECUTOR_URL  ?= http://localhost:8081

.PHONY: build run-executor run-agent supabase-setup lint vet fmt tidy

build:
	go build ./...

# Terminal 1: start the MCP Executor (owns DB connections + security boundary).
run-executor:
	EXECUTOR_ADDR=$(EXECUTOR_ADDR) go run ./cmd/executor

# Terminal 2: start the agent + interactive CLI (connects to the executor over MCP).
run-agent:
	EXECUTOR_URL=$(EXECUTOR_URL) go run ./cmd/agent

# Apply the Tesla demo schema + seed + read-only role to Supabase/Postgres.
# Use an owner/superuser connection string (direct port 5432):
#   make supabase-setup SUPABASE_DB_URL="postgresql://postgres:...@db.<ref>.supabase.co:5432/postgres"
supabase-setup:
	@test -n "$(SUPABASE_DB_URL)" || { echo "set SUPABASE_DB_URL=postgresql://postgres:...@host:5432/postgres"; exit 1; }
	psql "$(SUPABASE_DB_URL)" -f deploy/supabase/setup.sql

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed; using go vet"; go vet ./...; fi

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

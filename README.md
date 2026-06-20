# QueryLynx

Universal natural-language → query gateway. Connect any database; ask questions in plain English; get structured results or charts.

QueryLynx introspects your schema automatically, routes questions to the right tables via RAG, generates dialect-aware queries, validates them, and executes them safely. It supports SQL today; the architecture accommodates NoSQL, OLAP/lakehouse, and time-series backends without changing the core.

---

## Architecture

```
User question
  │
  ├─ Intent Classifier      fast model — determines modality (table | chart | scalar)
  ├─ Table Router (RAG)     finds relevant tables from introspected schema
  ├─ Text2SQL Agent         generates dialect-aware SQL, self-corrects up to 3×
  ├─ MCP Executor           security boundary: read-only creds + LIMIT + timeout
  ├─ Plot Agent             emits ECharts JSON spec (never a server-rendered image)
  └─ Explainability Record  structured per-stage trace transmitted to the frontend

Two binaries, one repo:
  cmd/agent     — orchestration, LLM calls, RAG, interactive CLI
  cmd/executor  — MCP executor, DB connections, query execution
```

The agent and executor are separate processes. The executor is a **real MCP server**
(Streamable HTTP); the agent is its MCP client. The agent holds the LLM key and never
touches a database; the executor holds the DB connection and never sees the LLM key.

The LLM is **never** a security boundary. The Executor enforces safety in code:
- read-only DB role (the role physically cannot write), **and**
- every query runs inside a `BEGIN ... READ ONLY` transaction with a `statement_timeout`,
  so even a write-capable DSN cannot write and no query can run unbounded.

---

## Status

Phase 0 (MVP) delivers a working end-to-end vertical slice against real Postgres/Supabase:
connect → introspect → route → text2sql (self-correct) → read-only execute → result + trace.

| Feature | Status |
|---------|--------|
| Postgres connector (Supabase / Neon / vanilla) | ✅ Phase 0 |
| Schema auto-explore (tables, columns, FKs, row counts, sample values) | ✅ Phase 0 |
| LLM-based table routing (< 100 tables) | ✅ Phase 0 |
| Text2SQL with self-correction (max 3, 30s SLA, per-stage timeouts) | ✅ Phase 0 |
| Hallucination check — table names (columns via DB error feedback) | ✅ Phase 0 |
| MCP Executor: real MCP server (Streamable HTTP) | ✅ Phase 0 |
| Security boundary: read-only role + read-only tx + LIMIT injection + timeout | ✅ Phase 0 |
| BYO key per connection session (Anthropic + OpenAI) | ✅ Phase 0 |
| Interactive CLI wizard + ask loop | ✅ Phase 0 |
| Explainability record (per-stage trace) returned with each answer | ✅ Phase 0 |
| Gemini provider | planned (0b) |
| Real intent classifier (cheap model) | planned (0b) |
| ECharts plot spec (text2plot) | planned (0b) |
| SSE streaming of stages to a frontend + web UI | planned (Phase 1) |
| Hallucination check — columns via sqlglot AST | planned (Phase 2) |
| MySQL / MariaDB connector | planned (Phase 1) |
| Docker Compose Tesla seed (Supabase used in Phase 0) | planned (Phase 1) |

---

## Roadmap

See **[ROADMAP.md](ROADMAP.md)** for the full milestone tracker. In short:

- **Phase 0 (current)** — MVP: NL → safe SQL → real rows against Postgres/Supabase, via CLI, executor as a real MCP server. *Code-complete; first live run pending.*
- **Phase 0b** — real intent classifier, Plot Agent (ECharts / text2plot), SSE streaming, Gemini.
- **Phase 1** — HTTP API + web UI, MySQL/MariaDB, Docker Compose, CI.
- **Phase 2** — embedding router (datalake scale), sqlglot AST, MongoDB/Trino/DuckDB, eval harness, observability.
- **Phase 3** — voice I/O + accessibility-first UX.

---

## Running QueryLynx (Phase 0)

QueryLynx runs as **two processes**:
- the **executor** (`cmd/executor`) — an MCP server that owns the DB connection and enforces read-only;
- the **agent** (`cmd/agent`) — LLM orchestration + the interactive CLI.

Start the executor first, then the agent. The agent connects to it over MCP and drops you into a connection wizard.

### Prerequisites
- Go 1.22+
- A Supabase (or any Postgres) project, with the owner connection string
- `psql` (to load the demo data)
- An Anthropic or OpenAI API key

### 1. Load the Tesla demo dataset + read-only role
```bash
make supabase-setup SUPABASE_DB_URL="postgresql://postgres:...@db.<ref>.supabase.co:5432/postgres"
# creates 4 tables (~6,270 rows) and the read-only role `querylynx_ro`
```
(You can also paste `deploy/supabase/setup.sql` into the Supabase SQL Editor.)
Change the `querylynx_ro` password in `setup.sql` before using anything but a throwaway project.

### 2. Start the two processes
```bash
# terminal 1 — the MCP executor (security boundary)
make run-executor

# terminal 2 — the agent + CLI
export ANTHROPIC_API_KEY=sk-ant-...   # optional; the wizard will prompt if unset
make run-agent
```

### 3. Connect and ask
The CLI wizard walks you through provider → API key → model → database → connection.
For the connection, use the read-only role on the **direct** port 5432:
```
postgresql://querylynx_ro:<password>@db.<ref>.supabase.co:5432/postgres?sslmode=require
```
On success you'll see the discovered schema, then a prompt:
```
✓ connected (postgres) — found 4 tables, 6270 rows
    vehicles                 30 rows
    charging_sessions        450 rows
    ...
Ask in plain English. Commands:  /trace   /tables   /exit

> How many vehicles per model?

SQL: SELECT model, COUNT(*) FROM vehicles GROUP BY model
...
```
CLI commands: `/trace` (show per-stage timings), `/tables` (show schema), `/exit`.

### Example questions (Tesla demo data)
- `How many vehicles per model?`
- `Which vehicle has the highest total kWh added?`
- `Total charging cost per location type`
- `Average trip distance per model`
- `Which vehicle took the most trips?`

---

## Seed Data

`deploy/supabase/setup.sql` loads a Tesla-flavoured synthetic dataset (enums, FKs, jsonb,
timestamptz, partial indexes):

| Table | Rows | Description |
|-------|------|-------------|
| `vehicles` | 30 | Model 3/Y/S/X, VIN, year, colour, purchase date |
| `charging_sessions` | ~450 | Per-vehicle charging history, kWh, location, cost |
| `trips` | ~750 | Per-vehicle trips, distance, energy, speed |
| `battery_telemetry` | ~5,040 | Hourly SOC, temp, voltage readings per vehicle |

Phase 1 will add a MySQL `init.sql` with the same data for cross-dialect parity.

---

## Configuration

All configuration is provided at connection registration time. The only env vars needed to start the binaries:

| Env var | Default | Description |
|---------|---------|-------------|
| `EXECUTOR_ADDR` | `:8081` | Executor MCP listen address |
| `EXECUTOR_URL` | `http://localhost:8081` | Agent → Executor MCP endpoint |
| `ANTHROPIC_API_KEY` | — | optional; pre-fills the wizard's key prompt |
| `OPENAI_API_KEY` | — | optional; pre-fills the wizard's key prompt |

LLM API keys are bound to a connection session and stored in memory only — never
persisted, logged, or included in traces.

---

## Explainability, Feedback, and Observability

Every query response should include an `explainability` record that the frontend can render without recomputing pipeline state. This is a structured execution trace, not the model's hidden chain-of-thought.

Recommended shape:
```json
{
  "trace_id": "qry_abc123",
  "stages": [
    {"name": "intent", "status": "ok", "output": {"modality": "chart", "in_scope": true}, "latency_ms": 182},
    {"name": "routing", "status": "ok", "output": {"tables": ["vehicles", "charging_sessions"]}, "latency_ms": 246},
    {"name": "generation", "status": "ok", "output": {"sql": "SELECT ...", "attempts": 1}, "latency_ms": 1210},
    {"name": "validation", "status": "ok", "output": {"limit_injected": true, "row_cap": 1000}, "latency_ms": 14},
    {"name": "execution", "status": "ok", "output": {"row_count": 12}, "latency_ms": 87}
  ]
}
```

After every assistant response, the UI should expose four actions: copy to clipboard, thumb up, thumb down, and explainability. Clicking explainability opens a right-side drawer showing the per-stage record as a vertical timeline of compact cards: question, security check, intent classification, table routing, query generation, validation, execution, plot generation, errors, and retries. Each card should show the stage status, key decision/output, confidence when available, and duration.

Observability traces may include stage names, model IDs, generated SQL, selected tables, attempts, latencies, row counts, and sanitized error messages. They must never include API keys, DSNs with credentials, database passwords, or raw prompts containing secrets.

---

## Development

```bash
make build               # build all packages
make supabase-setup      # load Tesla demo data (needs SUPABASE_DB_URL=...)
make run-executor        # start the MCP executor (terminal 1)
make run-agent           # start the agent + CLI (terminal 2)
make vet                 # go vet
make lint                # golangci-lint if installed, else go vet
```

Tests are deferred until Phase 1 test databases are set up (see
`.claude/skills/testing-standard`).

---

## Design Decisions

**Why not LangChain/LangGraph?** Hand-written orchestration is more observable, testable, and has no hidden abstractions. Every prompt and retry is visible in the trace.

**Why is the LLM not the security boundary?** LLM behaviour is probabilistic. A system prompt instruction can be overridden by adversarial input. The Executor enforces safety at the credential and structural level — neither can be jailbroken.

**Why an allowlist and not a denylist?** Denylists are bypassed trivially via comments, encoding tricks, and Unicode homoglyphs. An allowlist rejects everything not explicitly permitted.

**Why ECharts spec and not a rendered image?** Accuracy (structured JSON can't hallucinate axes), token cost, accessibility (spec carries alt-text and ARIA metadata), and frontend reuse.

**Why series pipeline and not parallel?** The intent classifier uses a cheap/fast model (Haiku-level), keeping intent latency under 400ms. This makes series latency negligible while saving tokens on out-of-scope requests.

**Why two binaries in one repo?** Agent and Executor have different security profiles and scaling needs. They communicate over **MCP (Streamable HTTP)** — the Executor is a real MCP server — so splitting into separate repos/deployments later is just a config change. One repo keeps development fast now.

**Why MCP for the agent↔executor link?** It makes "uses MCP" a literal, inspectable claim (4 typed tools over the standard protocol), not a label on a bespoke RPC. The executor could be driven by any MCP-capable host.

---

## Project Skills

See `.claude/skills/` for session-to-session guidance:
- `architecture-overview` — full vision, phase roadmap, key decisions
- `add-connector` — recipe for adding a new database backend
- `add-llm-provider` — recipe for adding a new LLM provider
- `safety-invariants` — non-negotiable security rules
- `testing-standard` — how and when to write tests

# QueryLynx Roadmap

Living milestone tracker. Legend: ✅ done · 🟡 in progress · ⬜ planned.

**Current position: Phase 0 MVP — code-complete, pending the first live run** against Supabase.

---

## Phase 0 — MVP (current)

**Goal:** prove the end-to-end vertical slice — natural language → safe SQL → real rows —
against a real Postgres/Supabase database, driven from an interactive CLI, with the executor
as a real MCP server.

**Risk retired:** can the core pipeline answer questions *correctly* and *safely* against a
real database?

### Delivered
- ✅ Postgres connector (Supabase / Neon / vanilla): introspect (tables, columns, types, FKs, row counts, sample values), validate, execute
- ✅ Security boundary in code: read-only role **+** `BEGIN … READ ONLY` tx **+** `statement_timeout` **+** LIMIT injection
- ✅ Executor is a real MCP server (Streamable HTTP, official `go-sdk`); agent is the MCP client; 4 tools
- ✅ Secret separation: LLM key lives only in the agent, DB DSN only in the executor
- ✅ LLM providers: Anthropic + OpenAI (raw net/http, no SDK) + provider registry
- ✅ RAG table router (LLM-based, for schemas < 100 tables)
- ✅ Text2SQL: dialect-aware prompt + self-correction loop (max 3, 30s SLA, per-stage timeouts)
- ✅ Hallucination check: table-level pre-execution; columns via DB error → self-correction
- ✅ Intent classifier (Phase 0 stub: table / in-scope)
- ✅ Connection registry / session
- ✅ Interactive CLI wizard (provider → key → model → dialect → connection) + ask loop (`/trace`, `/tables`, `/exit`)
- ✅ Explainability record (per-stage trace) returned with every answer
- ✅ Tesla demo dataset + `querylynx_ro` read-only role (`deploy/supabase/setup.sql`)
- ✅ Build / vet / gofmt green; MCP transport + CLI wizard smoke tests pass

### Pending
- 🟡 First live run against Supabase (needs an owner DSN to seed + an LLM API key)
- ⬜ Tune prompts / default model based on live results

**Acceptance:** from a fresh Supabase project → `make supabase-setup` → `make run-executor` +
`make run-agent` → ask the 5 demo questions and get correct rows; `/trace` shows per-stage
timings; a write attempt is blocked by the read-only transaction.

---

## Phase 0b — Complete the pipeline

**Goal:** both halves of the pitch work (text2query **and** text2plot); real gating.

- ⬜ Real intent classifier (cheap/fast model, < 400ms; modality + in-scope)
- ⬜ Plot Agent — emit ECharts JSON spec for chart-modality questions (never server-rendered images)
- ⬜ External SSE streaming of pipeline stages to the client (CLI live stages now; web later)
- ⬜ Gemini provider
- ⬜ Per-response actions plumbing (clipboard / 👍 / 👎 tied to `trace_id`)

---

## Phase 1 — Universal SQL + Web

**Goal:** more SQL dialects, an HTTP API, and a web UI; production-shaped.

- ⬜ `internal/httpapi`: `POST /connections`, `POST /query` (JSON + SSE) — same orchestrator as the CLI
- ⬜ Web UI: ask box, results table/chart, explainability drawer, feedback actions
- ⬜ MySQL / MariaDB connector (same Tesla data for cross-dialect parity)
- ⬜ Docker Compose (Postgres + MySQL) as a local alternative to Supabase
- ⬜ Human-editable table/column descriptions (augment auto-introspection)
- ⬜ CI (build, vet, lint)

---

## Phase 2 — Scale + correctness + new backends

- ⬜ Embedding-based table router for datalake schemas (100+ tables): OpenAI `text-embedding-3-small` + in-memory cosine; auto-selected by table count
- ⬜ sqlglot AST validation + dialect transpilation + column-reference hallucination check (Python sidecar)
- ⬜ MongoDB connector (text2MQL — proves cross-paradigm)
- ⬜ Trino/Iceberg connector; DuckDB connector
- ⬜ Eval harness: NL→gold set, execution accuracy (not string match), per-dialect/per-model breakdown
- ⬜ Grafana observability: traces + eval scores

---

## Phase 3 — Accessibility + voice

- ⬜ Audio2text input + TTS output → voice-driven analytics
- ⬜ Screen-reader-first UX: alt-text, ARIA labels, data table on every chart spec
- ⬜ Chart sonification for visually impaired users

---

## Milestone history
- **2026-06-15** — Phase 0 MVP implemented (25 Go files + `setup.sql`); MCP transport + CLI wizard verified by smoke tests; first live run pending.

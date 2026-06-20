// Package agent orchestrates the QueryLynx pipeline for one connection session:
// intent -> routing -> text2sql (+ self-correction) -> executor -> explainability.
// It holds no database code; execution goes through the MCP Executor.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"querylynx/internal/agent/intent"
	"querylynx/internal/agent/sql"
	"querylynx/internal/connector"
	"querylynx/internal/prompt"
	"querylynx/internal/rag"
	"querylynx/internal/schema"
)

const (
	totalSLA     = 30 * time.Second
	routeTimeout = 15 * time.Second
	genTimeout   = 15 * time.Second
	execTimeout  = 10 * time.Second
	maxAttempts  = 3
)

// QueryExecutor runs a query through the executor's security boundary
// (validate + read-only execute). Satisfied by executorclient.Client.
type QueryExecutor interface {
	ValidateAndExecute(ctx context.Context, connectionID, query string) (connector.Result, error)
}

// Stage is one entry in the explainability trace — a controllable decision, not
// hidden model chain-of-thought. Safe to send to the frontend.
type Stage struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // ok | error
	Output    any    `json:"output,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Explainability is the structured, frontend-renderable execution trace.
type Explainability struct {
	TraceID string  `json:"trace_id"`
	Stages  []Stage `json:"stages"`
}

// Answer is the result of a query plus its trace.
type Answer struct {
	SQL            string           `json:"sql"`
	Result         connector.Result `json:"result"`
	Attempts       int              `json:"attempts"`
	Explainability Explainability   `json:"explainability"`
}

// Pipeline holds the per-session stages and configuration.
type Pipeline struct {
	ConnectionID string
	Dialect      string
	Schema       schema.Schema
	Intent       intent.Classifier
	Router       rag.TableRouter
	Generator    *sql.Generator
	Executor     QueryExecutor
}

// Run executes the full pipeline for one question under a 30s total SLA.
func (p *Pipeline) Run(ctx context.Context, question string) (*Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, totalSLA)
	defer cancel()

	ans := &Answer{Explainability: Explainability{TraceID: newTraceID()}}

	// 1. Intent (gate).
	in, err := p.Intent.Classify(ctx, question)
	p.record(ans, "intent", map[string]any{"modality": in.Modality, "in_scope": in.InScope}, 0, err)
	if err != nil {
		return ans, err
	}
	if !in.InScope {
		return ans, fmt.Errorf("agent: question is out of scope")
	}

	// 2. Routing.
	tables, err := p.route(ctx, ans, question)
	if err != nil {
		return ans, err
	}
	selected := prompt.Subset(p.Schema, tables)

	// 3-5. Generate -> validate tables -> execute, with self-correction.
	var priorErr string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ans.Attempts = attempt

		query, gerr := p.generate(ctx, ans, question, selected, priorErr, attempt)
		if gerr != nil {
			return ans, gerr // an LLM/transport failure is not self-correctable
		}
		ans.SQL = query

		if verr := sql.ValidateTables(query, selected); verr != nil {
			p.record(ans, "validation", map[string]any{"attempt": attempt, "ok": false}, 0, verr)
			priorErr = verr.Error()
			continue
		}

		res, lat, eerr := p.execute(ctx, query)
		if eerr != nil {
			p.record(ans, "execution", map[string]any{"attempt": attempt}, lat, eerr)
			priorErr = eerr.Error()
			continue
		}
		p.record(ans, "execution", map[string]any{"attempt": attempt, "row_count": len(res.Rows)}, lat, nil)
		ans.Result = res
		return ans, nil
	}
	return ans, fmt.Errorf("agent: no valid query after %d attempts: %s", maxAttempts, priorErr)
}

func (p *Pipeline) route(ctx context.Context, ans *Answer, question string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	start := time.Now()
	tables, err := p.Router.Route(rctx, question, p.Schema)
	p.record(ans, "routing", map[string]any{"tables": tables}, time.Since(start).Milliseconds(), err)
	if err != nil {
		return nil, err
	}
	return tables, nil
}

func (p *Pipeline) generate(ctx context.Context, ans *Answer, question string, s schema.Schema, priorErr string, attempt int) (string, error) {
	gctx, cancel := context.WithTimeout(ctx, genTimeout)
	defer cancel()
	start := time.Now()
	query, err := p.Generator.Generate(gctx, p.Dialect, question, s, priorErr)
	p.record(ans, "generation", map[string]any{"attempt": attempt, "sql": query}, time.Since(start).Milliseconds(), err)
	return query, err
}

func (p *Pipeline) execute(ctx context.Context, query string) (connector.Result, int64, error) {
	ectx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	start := time.Now()
	res, err := p.Executor.ValidateAndExecute(ectx, p.ConnectionID, query)
	return res, time.Since(start).Milliseconds(), err
}

func (p *Pipeline) record(ans *Answer, name string, output any, latencyMS int64, err error) {
	st := Stage{Name: name, Output: output, LatencyMS: latencyMS, Status: "ok"}
	if err != nil {
		st.Status = "error"
		st.Error = err.Error()
	}
	ans.Explainability.Stages = append(ans.Explainability.Stages, st)
}

func newTraceID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "qry_" + hex.EncodeToString(b[:])
}

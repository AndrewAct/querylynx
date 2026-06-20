package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"querylynx/internal/llm"
	"querylynx/internal/prompt"
	"querylynx/internal/schema"
)

// LLMRouter asks the connection's own LLM to pick relevant tables. Zero extra cost
// (reuses the session's BYO key) and accurate for schemas under ~100 tables, which
// is the Phase 1 target. The schema's sample values make this surprisingly precise.
type LLMRouter struct {
	llm llm.Provider
}

// NewLLMRouter returns an LLMRouter backed by the given provider.
func NewLLMRouter(p llm.Provider) *LLMRouter { return &LLMRouter{llm: p} }

var _ TableRouter = (*LLMRouter)(nil)

// Route returns the table names relevant to the question. Hallucinated names (not
// in the schema) are dropped. If nothing usable comes back, it falls back to all
// tables — a small-schema safety net so a routing miss never kills the query.
func (r *LLMRouter) Route(ctx context.Context, question string, s schema.Schema) ([]string, error) {
	if len(s.Tables) == 0 {
		return nil, fmt.Errorf("rag: schema has no tables")
	}

	resp, err := r.llm.Generate(ctx, routingPrompt(question, s))
	if err != nil {
		return nil, fmt.Errorf("rag: routing generate: %w", err)
	}

	names, err := parseTableList(resp)
	if err != nil {
		return nil, err
	}

	valid := filterKnown(names, s)
	if len(valid) == 0 {
		return allTableNames(s), nil
	}
	return valid, nil
}

func routingPrompt(question string, s schema.Schema) string {
	return fmt.Sprintf(`You are a table router for a text-to-SQL system. Given a database schema and a user question, choose the minimal set of tables needed to answer it.

Schema:
%s
Question: %s

Return ONLY a JSON array of table names, for example ["vehicles","charging_sessions"]. No prose, no explanation.`,
		prompt.Schema(s), question)
}

// parseTableList extracts the JSON array from a model response that may wrap it in
// prose or code fences.
func parseTableList(resp string) ([]string, error) {
	start := strings.Index(resp, "[")
	end := strings.LastIndex(resp, "]")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("rag: no JSON array in routing response")
	}
	var names []string
	if err := json.Unmarshal([]byte(resp[start:end+1]), &names); err != nil {
		return nil, fmt.Errorf("rag: parse routing response: %w", err)
	}
	return names, nil
}

// filterKnown keeps only names that match a real table (case-insensitive).
func filterKnown(names []string, s schema.Schema) []string {
	known := make(map[string]string, len(s.Tables)) // lower -> canonical
	for _, t := range s.Tables {
		known[strings.ToLower(t.Name)] = t.Name
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if canon, ok := known[strings.ToLower(strings.TrimSpace(n))]; ok && !seen[canon] {
			out = append(out, canon)
			seen[canon] = true
		}
	}
	return out
}

func allTableNames(s schema.Schema) []string {
	out := make([]string, 0, len(s.Tables))
	for _, t := range s.Tables {
		out = append(out, t.Name)
	}
	return out
}

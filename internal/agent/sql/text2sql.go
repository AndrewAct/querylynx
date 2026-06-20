// Package sql is the Text2SQL stage: it turns a question plus the routed schema
// into a single read-only SQL query. It is a pure generate+extract step — the
// self-correction loop and execution live in the pipeline, so this stays testable
// and the pipeline keeps clean per-stage explainability.
package sql

import (
	"context"
	"fmt"
	"strings"

	"querylynx/internal/llm"
	"querylynx/internal/prompt"
	"querylynx/internal/schema"
)

// Generator produces SQL from natural language using the session's LLM.
type Generator struct {
	llm llm.Provider
}

// NewGenerator returns a Generator backed by the given provider.
func NewGenerator(p llm.Provider) *Generator { return &Generator{llm: p} }

// Generate writes a single read-only query for dialect that answers question over
// the given (already routed) schema. When priorErr is non-empty it is fed back so
// the model can self-correct a previous failed attempt.
func (g *Generator) Generate(ctx context.Context, dialect, question string, s schema.Schema, priorErr string) (string, error) {
	resp, err := g.llm.Generate(ctx, buildPrompt(dialect, question, s, priorErr))
	if err != nil {
		return "", fmt.Errorf("text2sql: generate: %w", err)
	}
	q := extractSQL(resp)
	if q == "" {
		return "", fmt.Errorf("text2sql: model returned no SQL")
	}
	return q, nil
}

func buildPrompt(dialect, question string, s schema.Schema, priorErr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are an expert %s analyst. Write a single read-only SQL query that answers the question.\n\n", dialect)
	b.WriteString("Rules:\n")
	b.WriteString("- Use ONLY the tables and columns in the schema below. Never invent names.\n")
	b.WriteString("- The query must be a single SELECT or WITH statement. No INSERT/UPDATE/DELETE/DDL, no multiple statements.\n")
	b.WriteString("- Return ONLY the SQL. You may wrap it in a ```sql code block.\n\n")
	fmt.Fprintf(&b, "Schema:\n%s\n", prompt.Schema(s))
	if priorErr != "" {
		fmt.Fprintf(&b, "Your previous attempt failed with this error:\n%s\nWrite a corrected query.\n\n", priorErr)
	}
	fmt.Fprintf(&b, "Question: %s\n", question)
	return b.String()
}

// extractSQL pulls the SQL out of a model response, stripping an optional
// ```sql ... ``` code fence and surrounding prose.
func extractSQL(resp string) string {
	resp = strings.TrimSpace(resp)
	i := strings.Index(resp, "```")
	if i < 0 {
		return resp
	}
	rest := resp[i+3:]
	// Drop an optional language tag on the fence's first line (e.g. "sql").
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		if firstLine := strings.TrimSpace(rest[:nl]); firstLine == "" || isWord(firstLine) {
			rest = rest[nl+1:]
		}
	}
	if j := strings.Index(rest, "```"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

func isWord(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

// Package rag selects the tables relevant to a question (the "Table Router" stage).
package rag

import (
	"context"

	"querylynx/internal/schema"
)

// TableRouter narrows a full schema to the tables needed to answer a question.
// Phase 1 default is LLMRouter (LLM picks from the table list). Phase 2 adds an
// EmbeddingRouter for datalake-scale schemas (100+ tables); the connection layer
// chooses the strategy by table count. Same interface either way.
type TableRouter interface {
	Route(ctx context.Context, question string, s schema.Schema) ([]string, error)
}

// Package intent is the pipeline's gate: it classifies a question's modality
// (table | chart | scalar) and whether it is in scope, before any expensive work.
package intent

import "context"

// Modality is what kind of answer the question wants.
type Modality string

const (
	ModalityTable  Modality = "table"
	ModalityChart  Modality = "chart"
	ModalityScalar Modality = "scalar"
)

// Result is an intent classification.
type Result struct {
	Modality Modality `json:"modality"`
	InScope  bool     `json:"in_scope"`
}

// Classifier gates the pipeline. The real one (Phase 0b) uses a cheap/fast model
// to keep intent latency under 400ms — which is what makes a series pipeline's
// latency penalty negligible.
type Classifier interface {
	Classify(ctx context.Context, question string) (Result, error)
}

// Stub is the Phase 0 classifier: always table / in-scope. Keeping the interface
// here means swapping in the real cheap-model classifier later is a one-line change
// in the pipeline, and the pipeline already handles the out-of-scope path.
type Stub struct{}

// NewStub returns a Stub classifier.
func NewStub() *Stub { return &Stub{} }

var _ Classifier = (*Stub)(nil)

// Classify always returns table modality, in scope.
func (Stub) Classify(_ context.Context, _ string) (Result, error) {
	return Result{Modality: ModalityTable, InScope: true}, nil
}

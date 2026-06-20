package llm

import "context"

// Provider is the interface every LLM backend must implement.
// Adding a provider: see .claude/skills/add-llm-provider/SKILL.md.
type Provider interface {
	// Generate sends a prompt and returns the model's text response.
	// The ctx deadline is respected; callers should set per-stage timeouts.
	Generate(ctx context.Context, prompt string) (string, error)

	// TODO(phase2): add GenerateStream(ctx context.Context, prompt string) (<-chan string, error)
}

// Config carries the neutral parameters needed to construct a provider. The
// dialect-specific constructor (e.g. anthropic.New) translates this into its own
// config. Lives here (not in a subpackage) so it has no provider imports and the
// registry package can depend on both this and the providers without a cycle.
type Config struct {
	APIKey  string // bound to a connection session; memory-only, never logged or persisted
	ModelID string // empty => provider default
}

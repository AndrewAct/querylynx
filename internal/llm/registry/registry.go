// Package registry maps a provider name to a concrete llm.Provider. It lives in
// its own package (not in package llm) to avoid an import cycle: the providers
// import llm, so llm cannot import them back.
package registry

import (
	"fmt"

	"querylynx/internal/llm"
	"querylynx/internal/llm/anthropic"
	"querylynx/internal/llm/openai"
)

// New constructs a provider with a session-bound API key. Adding a provider:
// implement llm.Provider under internal/llm/<name>/ and add a case here.
// See .claude/skills/add-llm-provider/SKILL.md.
func New(provider string, cfg llm.Config) (llm.Provider, error) {
	switch provider {
	case "anthropic":
		return anthropic.New(anthropic.Config{APIKey: cfg.APIKey, ModelID: cfg.ModelID})
	case "openai":
		return openai.New(openai.Config{APIKey: cfg.APIKey, ModelID: cfg.ModelID})
	case "gemini":
		return nil, fmt.Errorf("llm: provider %q is coming soon", provider)
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", provider)
	}
}

// DefaultModel returns the default model id for a provider, for display in the
// CLI wizard. Empty for providers without a wired default.
func DefaultModel(provider string) string {
	switch provider {
	case "anthropic":
		return anthropic.DefaultModelID
	case "openai":
		return openai.DefaultModelID
	default:
		return ""
	}
}

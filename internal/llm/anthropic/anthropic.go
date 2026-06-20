// Package anthropic implements llm.Provider against the Anthropic Messages API.
// Raw net/http + encoding/json — no SDK — so every request and response is visible
// and there are no hidden abstractions in the critical path.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"querylynx/internal/llm"
)

// DefaultModelID is used when Config.ModelID is empty. Update when deprecated.
// Haiku is fast/cheap; users can pass a stronger model (Sonnet/Opus) for hard SQL.
const DefaultModelID = "claude-haiku-4-5-20251001"

const (
	defaultURL = "https://api.anthropic.com/v1/messages"
	apiVersion = "2023-06-01"
	maxTokens  = 4096
)

// Config is the Anthropic-specific provider config.
type Config struct {
	APIKey  string // required; never logged
	ModelID string // empty => DefaultModelID
	BaseURL string // empty => defaultURL; overridable for tests
}

// Provider is an Anthropic-backed llm.Provider.
type Provider struct {
	apiKey  string // unexported, never logged or returned
	modelID string
	baseURL string
	client  *http.Client
}

var _ llm.Provider = (*Provider)(nil)

// New constructs a Provider. The API key is required.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: API key is required")
	}
	if cfg.ModelID == "" {
		cfg.ModelID = DefaultModelID
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultURL
	}
	return &Provider{
		apiKey:  cfg.APIKey,
		modelID: cfg.ModelID,
		baseURL: cfg.BaseURL,
		client:  &http.Client{Timeout: 120 * time.Second}, // hard cap; ctx timeout wins in practice
	}, nil
}

func (p *Provider) Generate(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model":      p.modelID,
		"max_tokens": maxTokens,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("anthropic: new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

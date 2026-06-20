// Package openai implements llm.Provider against the OpenAI Chat Completions API.
// Raw net/http + encoding/json — no SDK.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"querylynx/internal/llm"
)

// DefaultModelID is used when Config.ModelID is empty. Update when deprecated.
const DefaultModelID = "gpt-4o-mini"

const defaultURL = "https://api.openai.com/v1/chat/completions"

// Config is the OpenAI-specific provider config.
type Config struct {
	APIKey  string // required; never logged
	ModelID string // empty => DefaultModelID
	BaseURL string // empty => defaultURL; overridable for tests
}

// Provider is an OpenAI-backed llm.Provider.
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
		return nil, fmt.Errorf("openai: API key is required")
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
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *Provider) Generate(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model":    p.modelID,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("openai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("openai: new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: status %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("openai: unmarshal response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: response contained no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

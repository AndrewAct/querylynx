// Package session is the connection registry. It binds a BYO LLM key and a database
// connection into a ready-to-query Pipeline. Secret hygiene is enforced by shape:
//   - the API key lives only inside the llm.Provider (an unexported field),
//   - the DSN is handed to the executor and then dropped — it is never stored here.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"querylynx/internal/agent"
	"querylynx/internal/agent/intent"
	"querylynx/internal/agent/sql"
	"querylynx/internal/executorclient"
	"querylynx/internal/llm"
	llmregistry "querylynx/internal/llm/registry"
	"querylynx/internal/rag"
	"querylynx/internal/schema"
)

// Session is one registered connection: a database + an LLM, ready to answer
// questions via Pipeline.
type Session struct {
	ConnectionID string
	Dialect      string
	Schema       schema.Schema
	Pipeline     *agent.Pipeline
}

// Registry holds active sessions and the shared MCP client to the executor.
type Registry struct {
	mu       sync.RWMutex
	exec     *executorclient.Client
	sessions map[string]*Session
}

// NewRegistry returns a registry backed by a connected executor client.
func NewRegistry(exec *executorclient.Client) *Registry {
	return &Registry{exec: exec, sessions: make(map[string]*Session)}
}

// RegisterParams is the connection registration input. APIKey and DSN are consumed
// during Register and never retained on the Session.
type RegisterParams struct {
	Provider string
	APIKey   string
	ModelID  string
	Dialect  string
	DSN      string
}

// Register builds the LLM provider, opens the (read-only) DB connection on the
// executor, introspects its schema, and assembles a Pipeline. On any failure after
// the connection is opened, it is closed so we don't leak executor-side state.
func (r *Registry) Register(ctx context.Context, p RegisterParams) (*Session, error) {
	provider, err := llmregistry.New(p.Provider, llm.Config{APIKey: p.APIKey, ModelID: p.ModelID})
	if err != nil {
		return nil, err
	}

	connID := newConnectionID()
	if err := r.exec.OpenConnection(ctx, connID, p.Dialect, p.DSN); err != nil {
		return nil, err
	}
	sc, err := r.exec.Introspect(ctx, connID)
	if err != nil {
		_ = r.exec.CloseConnection(ctx, connID)
		return nil, err
	}

	sess := &Session{
		ConnectionID: connID,
		Dialect:      p.Dialect,
		Schema:       sc,
		Pipeline: &agent.Pipeline{
			ConnectionID: connID,
			Dialect:      p.Dialect,
			Schema:       sc,
			Intent:       intent.NewStub(),
			Router:       rag.NewLLMRouter(provider),
			Generator:    sql.NewGenerator(provider),
			Executor:     r.exec,
		},
	}

	r.mu.Lock()
	r.sessions[connID] = sess
	r.mu.Unlock()
	return sess, nil
}

// Get returns a registered session.
func (r *Registry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Close tears down a session and its executor-side connection.
func (r *Registry) Close(ctx context.Context, id string) error {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
	return r.exec.CloseConnection(ctx, id)
}

func newConnectionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "conn_" + hex.EncodeToString(b[:])
}

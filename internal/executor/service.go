// Package executor is the MCP Executor's core: it owns live database connections
// and is the component that actually touches user databases. The security boundary
// lives here — read-only credentials plus the connector's read-only transaction.
// The agent never touches a database directly; it goes through this service (over MCP).
package executor

import (
	"context"
	"fmt"
	"sync"

	"querylynx/internal/connector"
	"querylynx/internal/connector/registry"
	"querylynx/internal/schema"
)

// Service holds one open Connector per registered connection_id.
type Service struct {
	mu    sync.RWMutex
	conns map[string]connector.Connector
}

// NewService returns an empty Service.
func NewService() *Service {
	return &Service{conns: make(map[string]connector.Connector)}
}

// OpenConnection opens a read-only connection for dialect/dsn and registers it
// under connectionID. New() pings, so a bad DSN fails here with a clear error.
// If connectionID is already registered, the previous connection is closed.
func (s *Service) OpenConnection(ctx context.Context, connectionID, dialect, dsn string) error {
	if connectionID == "" {
		return fmt.Errorf("executor: connection_id is required")
	}
	c, err := registry.New(ctx, dialect, connector.Config{DSN: dsn})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.conns[connectionID]; ok {
		_ = old.Close()
	}
	s.conns[connectionID] = c
	return nil
}

// Introspect returns the schema for a registered connection.
func (s *Service) Introspect(ctx context.Context, connectionID string) (schema.Schema, error) {
	c, err := s.get(connectionID)
	if err != nil {
		return schema.Schema{}, err
	}
	return c.Introspect(ctx)
}

// ValidateAndExecute is the security gate. Validate runs the structural allowlist
// and injects LIMIT; Execute runs read-only. Both run here, in code, after the LLM
// has generated the query — so a jailbroken model cannot bypass either step.
func (s *Service) ValidateAndExecute(ctx context.Context, connectionID, query string) (connector.Result, error) {
	c, err := s.get(connectionID)
	if err != nil {
		return connector.Result{}, err
	}
	safe, err := c.Validate(ctx, query)
	if err != nil {
		return connector.Result{}, err
	}
	return c.Execute(ctx, safe)
}

// CloseConnection closes and unregisters a connection. Unknown ids are a no-op.
func (s *Service) CloseConnection(connectionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.conns[connectionID]
	if !ok {
		return nil
	}
	delete(s.conns, connectionID)
	return c.Close()
}

func (s *Service) get(connectionID string) (connector.Connector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conns[connectionID]
	if !ok {
		return nil, fmt.Errorf("executor: unknown connection_id %q", connectionID)
	}
	return c, nil
}

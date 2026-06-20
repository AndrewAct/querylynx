package connector

import (
	"context"
	"querylynx/internal/schema"
)

// Result holds the output of a successful query execution.
type Result struct {
	Columns []string
	Rows    [][]any
}

// Config carries the neutral parameters needed to open a backend connection.
// The dialect-specific constructor (e.g. postgres.New) translates this into its
// own config. Lives here (not in a subpackage) so it has no dialect imports and
// the registry package can depend on both this and the dialect impls without a cycle.
type Config struct {
	DSN    string
	RowCap int // 0 => connector default (1000)
}

// Connector is the interface every database backend must implement.
// Adding a backend: see .claude/skills/add-connector/SKILL.md.
type Connector interface {
	// Dialect returns the backend name, e.g. "postgres", "mysql", "mongodb".
	Dialect() string

	// QueryLang returns the query language, e.g. "SQL", "MQL".
	QueryLang() string

	// Introspect returns a dialect-neutral description of the database schema.
	Introspect(ctx context.Context) (schema.Schema, error)

	// Validate runs the backend-specific safety check on the generated query.
	// It returns a sanitized query (e.g. with LIMIT injected) or an error.
	Validate(ctx context.Context, query string) (string, error)

	// Execute runs a validated, read-only query and returns structured results.
	Execute(ctx context.Context, query string) (Result, error)

	// Close releases the underlying connection.
	Close() error
}

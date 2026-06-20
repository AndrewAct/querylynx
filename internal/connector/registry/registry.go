// Package registry maps a dialect string to a concrete Connector. It lives in its
// own package (not in package connector) to avoid an import cycle: the dialect
// implementations import connector, so connector cannot import them back.
package registry

import (
	"context"
	"fmt"

	"querylynx/internal/connector"
	"querylynx/internal/connector/postgres"
)

// New opens a Connector for the given dialect. Adding a backend: implement the
// Connector interface under internal/connector/<name>/ and add a case here.
// See .claude/skills/add-connector/SKILL.md.
func New(ctx context.Context, dialect string, cfg connector.Config) (connector.Connector, error) {
	switch dialect {
	case "postgres", "postgresql":
		return postgres.New(ctx, postgres.Config{DSN: cfg.DSN, RowCap: cfg.RowCap})
	case "mysql", "mariadb":
		return nil, fmt.Errorf("connector: dialect %q is coming soon", dialect)
	case "mongodb":
		return nil, fmt.Errorf("connector: dialect %q is coming soon", dialect)
	default:
		return nil, fmt.Errorf("connector: unknown dialect %q", dialect)
	}
}

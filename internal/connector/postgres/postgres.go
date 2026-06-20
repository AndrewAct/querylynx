// Package postgres implements the Connector interface for PostgreSQL-compatible
// backends (vanilla Postgres, Supabase, Neon). It is used exclusively by the MCP
// Executor — never by the agent directly.
package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"querylynx/internal/connector"
	"querylynx/internal/safety"
	"querylynx/internal/schema"
)

const (
	defaultRowCap        = 1000
	statementTimeout     = 10 * time.Second
	sampleCardinalityMax = 50 // columns with <= this many distinct values get sample values
	sampleLimit          = 5  // how many sample values to keep per low-cardinality column
)

var limitRe = regexp.MustCompile(`(?i)\blimit\b`)

// Config is the postgres-specific connection config.
type Config struct {
	DSN    string // read-only DSN (the Executor connects as the read-only role)
	RowCap int    // injected LIMIT when a query has none; default 1000
}

// Connector is a read-only Postgres backend.
type Connector struct {
	pool  *pgxpool.Pool
	guard *safety.Guard
	cfg   Config
}

var _ connector.Connector = (*Connector)(nil)

// New opens a connection pool and verifies connectivity. It fails fast with a
// clear error so connection registration can surface "can't connect" to the user.
func New(ctx context.Context, cfg Config) (*Connector, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}
	if cfg.RowCap <= 0 {
		cfg.RowCap = defaultRowCap
	}
	pool, err := pgxpool.New(ctx, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Connector{pool: pool, guard: safety.New(), cfg: cfg}, nil
}

func (c *Connector) Dialect() string   { return "postgres" }
func (c *Connector) QueryLang() string { return "SQL" }

// Introspect returns a dialect-neutral schema: tables, columns, types, foreign
// keys, approximate row counts, and sample values for low-cardinality columns.
func (c *Connector) Introspect(ctx context.Context) (schema.Schema, error) {
	var out schema.Schema

	if err := c.pool.QueryRow(ctx, `select current_database()`).Scan(&out.DatabaseName); err != nil {
		return out, fmt.Errorf("postgres: current_database: %w", err)
	}

	tableIdx, err := c.introspectColumns(ctx, &out)
	if err != nil {
		return out, err
	}
	if err := c.introspectForeignKeys(ctx, &out, tableIdx); err != nil {
		return out, err
	}

	// Per-table row count + per-column sample values.
	for i := range out.Tables {
		t := &out.Tables[i]
		countSQL := fmt.Sprintf(`select count(*) from public.%s`, pgx.Identifier{t.Name}.Sanitize())
		if err := c.pool.QueryRow(ctx, countSQL).Scan(&t.RowCount); err != nil {
			return out, fmt.Errorf("postgres: count %s: %w", t.Name, err)
		}
		for j := range t.Columns {
			samples, err := c.sampleValues(ctx, t.Name, t.Columns[j].Name)
			if err != nil {
				return out, err
			}
			t.Columns[j].SampleValues = samples
		}
	}
	return out, nil
}

func (c *Connector) introspectColumns(ctx context.Context, out *schema.Schema) (map[string]int, error) {
	const q = `
		select t.table_name, c.column_name, c.data_type, (c.is_nullable = 'YES')
		from information_schema.tables t
		join information_schema.columns c
		  on c.table_schema = t.table_schema and c.table_name = t.table_name
		where t.table_schema = 'public' and t.table_type = 'BASE TABLE'
		order by t.table_name, c.ordinal_position`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("postgres: query columns: %w", err)
	}
	defer rows.Close()

	idx := map[string]int{}
	for rows.Next() {
		var tname, cname, dtype string
		var nullable bool
		if err := rows.Scan(&tname, &cname, &dtype, &nullable); err != nil {
			return nil, fmt.Errorf("postgres: scan column: %w", err)
		}
		ti, ok := idx[tname]
		if !ok {
			ti = len(out.Tables)
			idx[tname] = ti
			out.Tables = append(out.Tables, schema.Table{Name: tname})
		}
		out.Tables[ti].Columns = append(out.Tables[ti].Columns, schema.Column{
			Name: cname, DataType: dtype, Nullable: nullable,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate columns: %w", err)
	}
	return idx, nil
}

func (c *Connector) introspectForeignKeys(ctx context.Context, out *schema.Schema, idx map[string]int) error {
	const q = `
		select kcu.table_name, kcu.column_name, ccu.table_name, ccu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
		  on kcu.constraint_name = tc.constraint_name and kcu.table_schema = tc.table_schema
		join information_schema.constraint_column_usage ccu
		  on ccu.constraint_name = tc.constraint_name and ccu.table_schema = tc.table_schema
		where tc.constraint_type = 'FOREIGN KEY' and tc.table_schema = 'public'`
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("postgres: query foreign keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tname, col, refTab, refCol string
		if err := rows.Scan(&tname, &col, &refTab, &refCol); err != nil {
			return fmt.Errorf("postgres: scan fk: %w", err)
		}
		if ti, ok := idx[tname]; ok {
			out.Tables[ti].ForeignKeys = append(out.Tables[ti].ForeignKeys, schema.ForeignKey{
				Column: col, RefTable: refTab, RefColumn: refCol,
			})
		}
	}
	return rows.Err()
}

// sampleValues returns up to sampleLimit distinct values for a column, but only
// if the column is low-cardinality (<= sampleCardinalityMax distinct). High-card
// columns (ids, timestamps, free text) return nil — sample values are only useful
// as enum-like hints for routing and WHERE-clause generation.
func (c *Connector) sampleValues(ctx context.Context, table, column string) ([]string, error) {
	q := fmt.Sprintf(
		`select distinct %s::text from public.%s where %s is not null limit %d`,
		pgx.Identifier{column}.Sanitize(), pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{column}.Sanitize(), sampleCardinalityMax+1,
	)
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("postgres: sample %s.%s: %w", table, column, err)
	}
	defer rows.Close()

	var vals []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("postgres: scan sample %s.%s: %w", table, column, err)
		}
		vals = append(vals, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(vals) > sampleCardinalityMax {
		return nil, nil // high-cardinality: not a useful hint
	}
	if len(vals) > sampleLimit {
		vals = vals[:sampleLimit]
	}
	return vals, nil
}

// Validate runs the structural allowlist (safety.Guard) and injects a LIMIT when
// the query has none. This is the Connector's responsibility because LIMIT syntax
// is dialect-specific — it is NOT the upstream Safety Guard's job.
//
// TODO(phase2): replace string-based checks with sqlglot AST validation + transpilation.
func (c *Connector) Validate(_ context.Context, query string) (string, error) {
	if err := c.guard.Check(query); err != nil {
		return "", err
	}
	q := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	if !limitRe.MatchString(q) {
		q = fmt.Sprintf("%s LIMIT %d", q, c.cfg.RowCap)
	}
	return q, nil
}

// Execute runs a pre-validated read-only query. Two hard, code-enforced controls
// live here so a jailbroken upstream LLM cannot cause writes or runaway cost, even
// if the user registered a write-capable DSN:
//
//  1. BEGIN ... READ ONLY  — any INSERT/UPDATE/DELETE/DDL errors at the DB level.
//  2. SET LOCAL statement_timeout — bounds runtime (DoS backstop).
//
// We always ROLLBACK: a SELECT has nothing to commit, and rollback is correct on
// both success and error.
func (c *Connector) Execute(ctx context.Context, query string) (connector.Result, error) {
	var res connector.Result

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return res, fmt.Errorf("postgres: begin read-only tx: %w", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx, fmt.Sprintf("set local statement_timeout = %d", statementTimeout.Milliseconds())); err != nil {
		return res, fmt.Errorf("postgres: set statement_timeout: %w", err)
	}

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return res, fmt.Errorf("postgres: execute: %w", err)
	}
	defer rows.Close()

	for _, fd := range rows.FieldDescriptions() {
		res.Columns = append(res.Columns, fd.Name)
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return res, fmt.Errorf("postgres: read row: %w", err)
		}
		for i := range vals {
			vals[i] = normalize(vals[i])
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("postgres: iterate rows: %w", err)
	}
	return res, nil
}

func (c *Connector) Close() error {
	c.pool.Close()
	return nil
}

// normalize converts pgx/pgtype values into JSON-friendly Go types so results
// serialize cleanly over MCP and print cleanly in the CLI.
func normalize(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	case pgtype.Numeric:
		if f, err := x.Float64Value(); err == nil && f.Valid {
			return f.Float64
		}
		if dv, err := x.Value(); err == nil {
			return dv
		}
		return nil
	default:
		return v
	}
}

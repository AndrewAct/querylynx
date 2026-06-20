// Package protocol defines the wire DTOs and tool names shared between the MCP
// Executor server (internal/executor) and the agent-side client
// (internal/executorclient). It deliberately has no database imports (no pgx),
// so the agent binary that imports it stays free of DB driver dependencies.
package protocol

import (
	"querylynx/internal/connector"
	"querylynx/internal/schema"
)

// Server identity advertised over MCP.
const (
	ServerName    = "querylynx-executor"
	ServerVersion = "0.1.0"
)

// Tool names exposed by the Executor MCP server.
const (
	ToolOpenConnection     = "open_connection"
	ToolIntrospect         = "introspect"
	ToolValidateAndExecute = "validate_and_execute"
	ToolCloseConnection    = "close_connection"
)

// OpenConnectionInput opens a read-only DB connection under a connection id.
type OpenConnectionInput struct {
	ConnectionID string `json:"connection_id" jsonschema:"unique id for this connection session"`
	Dialect      string `json:"dialect" jsonschema:"database dialect, e.g. postgres"`
	DSN          string `json:"dsn" jsonschema:"read-only data source name / connection string"`
}

// OpenConnectionOutput acknowledges a successful open.
type OpenConnectionOutput struct {
	OK bool `json:"ok"`
}

// IntrospectInput identifies the connection to introspect.
type IntrospectInput struct {
	ConnectionID string `json:"connection_id"`
}

// IntrospectOutput carries the dialect-neutral schema.
type IntrospectOutput struct {
	Schema schema.Schema `json:"schema"`
}

// ValidateAndExecuteInput is a query to validate and read-only-execute.
type ValidateAndExecuteInput struct {
	ConnectionID string `json:"connection_id"`
	Query        string `json:"query" jsonschema:"the SQL query to validate and execute"`
}

// ValidateAndExecuteOutput carries the query result.
type ValidateAndExecuteOutput struct {
	Result connector.Result `json:"result"`
}

// CloseConnectionInput identifies the connection to close.
type CloseConnectionInput struct {
	ConnectionID string `json:"connection_id"`
}

// CloseConnectionOutput acknowledges a close.
type CloseConnectionOutput struct {
	OK bool `json:"ok"`
}

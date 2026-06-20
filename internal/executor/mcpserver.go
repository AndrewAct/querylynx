package executor

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"querylynx/internal/executor/protocol"
)

// NewMCPServer exposes a Service as a real MCP server. The agent (an MCP client)
// drives the executor entirely through these four tools — it never imports the
// connector/pgx code. This makes "uses MCP" a literal, inspectable claim, and keeps
// the security boundary (read-only creds + read-only tx) on the far side of a
// process / protocol boundary from the LLM orchestration.
//
// Tool handlers return their output as `any` (not a typed Out): the SDK only builds
// and enforces an output JSON schema when Out is a concrete type, and our outputs
// embed nested slices that marshal to JSON null when empty. Returning `any` ships
// the structured JSON without tripping output-schema validation. Inputs stay typed,
// so the tools still advertise precise input schemas.
func NewMCPServer(svc *Service) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: protocol.ServerName, Version: protocol.ServerVersion}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        protocol.ToolOpenConnection,
		Description: "Open a read-only database connection and register it under a connection id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.OpenConnectionInput) (*mcp.CallToolResult, any, error) {
		if err := svc.OpenConnection(ctx, in.ConnectionID, in.Dialect, in.DSN); err != nil {
			return nil, nil, err
		}
		return nil, protocol.OpenConnectionOutput{OK: true}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        protocol.ToolIntrospect,
		Description: "Introspect a registered connection: tables, columns, types, foreign keys, row counts, sample values.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.IntrospectInput) (*mcp.CallToolResult, any, error) {
		sc, err := svc.Introspect(ctx, in.ConnectionID)
		if err != nil {
			return nil, nil, err
		}
		return nil, protocol.IntrospectOutput{Schema: sc}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        protocol.ToolValidateAndExecute,
		Description: "Validate (allowlist + LIMIT injection) and read-only-execute a query. This is the security boundary.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in protocol.ValidateAndExecuteInput) (*mcp.CallToolResult, any, error) {
		res, err := svc.ValidateAndExecute(ctx, in.ConnectionID, in.Query)
		if err != nil {
			return nil, nil, err
		}
		return nil, protocol.ValidateAndExecuteOutput{Result: res}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        protocol.ToolCloseConnection,
		Description: "Close and unregister a connection.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in protocol.CloseConnectionInput) (*mcp.CallToolResult, any, error) {
		if err := svc.CloseConnection(in.ConnectionID); err != nil {
			return nil, nil, err
		}
		return nil, protocol.CloseConnectionOutput{OK: true}, nil
	})

	return s
}

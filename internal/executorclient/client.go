// Package executorclient is the agent-side handle to the MCP Executor. It speaks
// MCP (Streamable HTTP) and exposes typed methods that mirror the executor's tools,
// so the rest of the agent never deals with raw tool calls. It imports only the
// shared protocol DTOs — no pgx, no connector implementations.
package executorclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"querylynx/internal/connector"
	"querylynx/internal/executor/protocol"
	"querylynx/internal/schema"
)

// Client wraps a connected MCP client session to the executor.
type Client struct {
	session *mcp.ClientSession
}

// Dial connects to the executor's MCP endpoint (Streamable HTTP, e.g.
// http://localhost:8081) and performs the MCP initialize handshake.
func Dial(ctx context.Context, endpoint string) (*Client, error) {
	c := mcp.NewClient(&mcp.Implementation{Name: "querylynx-agent", Version: "0.1.0"}, nil)
	session, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		return nil, fmt.Errorf("executorclient: connect %s: %w", endpoint, err)
	}
	return &Client{session: session}, nil
}

// Close ends the MCP session.
func (c *Client) Close() error { return c.session.Close() }

// OpenConnection asks the executor to open a read-only DB connection.
func (c *Client) OpenConnection(ctx context.Context, connectionID, dialect, dsn string) error {
	_, err := call[protocol.OpenConnectionOutput](ctx, c, protocol.ToolOpenConnection, protocol.OpenConnectionInput{
		ConnectionID: connectionID, Dialect: dialect, DSN: dsn,
	})
	return err
}

// Introspect returns the schema of a registered connection.
func (c *Client) Introspect(ctx context.Context, connectionID string) (schema.Schema, error) {
	out, err := call[protocol.IntrospectOutput](ctx, c, protocol.ToolIntrospect, protocol.IntrospectInput{ConnectionID: connectionID})
	if err != nil {
		return schema.Schema{}, err
	}
	return out.Schema, nil
}

// ValidateAndExecute runs a query through the executor's security boundary.
func (c *Client) ValidateAndExecute(ctx context.Context, connectionID, query string) (connector.Result, error) {
	out, err := call[protocol.ValidateAndExecuteOutput](ctx, c, protocol.ToolValidateAndExecute, protocol.ValidateAndExecuteInput{
		ConnectionID: connectionID, Query: query,
	})
	if err != nil {
		return connector.Result{}, err
	}
	return out.Result, nil
}

// CloseConnection closes a registered connection.
func (c *Client) CloseConnection(ctx context.Context, connectionID string) error {
	_, err := call[protocol.CloseConnectionOutput](ctx, c, protocol.ToolCloseConnection, protocol.CloseConnectionInput{ConnectionID: connectionID})
	return err
}

// call invokes a tool, surfaces tool-level errors (IsError) as Go errors, and
// decodes the structured JSON output into Out.
func call[Out any](ctx context.Context, c *Client, name string, args any) (Out, error) {
	var out Out
	res, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return out, fmt.Errorf("executorclient: call %s: %w", name, err)
	}
	if res.IsError {
		return out, fmt.Errorf("executor %s: %s", name, errorText(res))
	}
	if err := decodeStructured(res, &out); err != nil {
		return out, fmt.Errorf("executorclient: decode %s output: %w", name, err)
	}
	return out, nil
}

// decodeStructured pulls the tool's structured output into out. The SDK mirrors
// structured output into both StructuredContent (preferred) and a JSON TextContent.
func decodeStructured(res *mcp.CallToolResult, out any) error {
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok && tc.Text != "" {
			return json.Unmarshal([]byte(tc.Text), out)
		}
	}
	return errors.New("no structured content in tool result")
}

func errorText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 {
		return "unknown executor error"
	}
	return b.String()
}

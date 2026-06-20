// Command executor is the QueryLynx MCP Executor: a standalone MCP server that
// owns database connections and enforces the read-only security boundary. The
// agent connects to it as an MCP client over Streamable HTTP.
//
//	Start it:  make run-executor   (or: go run ./cmd/executor)
//	Listens on EXECUTOR_ADDR (default :8081).
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"querylynx/internal/executor"
)

func main() {
	addr := os.Getenv("EXECUTOR_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	svc := executor.NewService()
	server := executor.NewMCPServer(svc)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	log.Printf("querylynx executor (MCP, Streamable HTTP) listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("executor: %v", err)
	}
}

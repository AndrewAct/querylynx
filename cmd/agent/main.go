// Command agent is the QueryLynx orchestrator + interactive CLI. It connects to
// the MCP Executor, runs a connection wizard (provider -> key -> model -> dialect
// -> connection), then drops into an ask loop that runs the text2sql pipeline.
//
//	Start the executor first (make run-executor), then:  make run-agent
//	Connects to EXECUTOR_URL (default http://localhost:8081).
package main

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"querylynx/internal/agent"
	"querylynx/internal/connector"
	"querylynx/internal/executorclient"
	llmregistry "querylynx/internal/llm/registry"
	"querylynx/internal/schema"
	"querylynx/internal/session"
)

func main() {
	execURL := getenv("EXECUTOR_URL", "http://localhost:8081")
	ctx := context.Background()

	fmt.Println("QueryLynx — natural-language → query gateway (Phase 0)")
	fmt.Printf("connecting to executor at %s ...\n", execURL)

	client, err := executorclient.Dial(ctx, execURL)
	if err != nil {
		fmt.Printf("\n✗ cannot reach the MCP executor at %s\n", execURL)
		fmt.Printf("  start it first in another terminal:  make run-executor\n  error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	reg := session.NewRegistry(client)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1<<20)

	sess := connectWizard(ctx, reg, in)
	askLoop(ctx, sess, in)
}

// ----------------------------------------------------------------------------
// Connection wizard
// ----------------------------------------------------------------------------

func connectWizard(ctx context.Context, reg *session.Registry, in *bufio.Scanner) *session.Session {
	for {
		provider := chooseProvider(in)
		apiKey := readAPIKey(provider, in)
		model := promptDefault(in, fmt.Sprintf("model [%s]", orDefault(llmregistry.DefaultModel(provider), "provider default")), "")
		dialect := chooseDialect(in)
		dsn := buildDSN(dialect, in)

		fmt.Println("\nconnecting & exploring schema ...")
		sess, err := reg.Register(ctx, session.RegisterParams{
			Provider: provider, APIKey: apiKey, ModelID: model, Dialect: dialect, DSN: dsn,
		})
		if err != nil {
			fmt.Printf("✗ connection failed: %v\n\nlet's try again.\n", err)
			continue
		}

		var total int64
		for _, t := range sess.Schema.Tables {
			total += t.RowCount
		}
		fmt.Printf("\n✓ connected (%s) — found %d tables, %d rows\n", sess.Dialect, len(sess.Schema.Tables), total)
		for _, t := range sess.Schema.Tables {
			fmt.Printf("    %-24s %d rows\n", t.Name, t.RowCount)
		}
		return sess
	}
}

func chooseProvider(in *bufio.Scanner) string {
	fmt.Println("\nLLM provider:")
	fmt.Println("  1) Anthropic")
	fmt.Println("  2) OpenAI")
	fmt.Println("  3) Gemini (coming soon)")
	for {
		switch choose(in, "  choose [1]: ", 3, 1) {
		case 1:
			return "anthropic"
		case 2:
			return "openai"
		default:
			fmt.Println("  Gemini is coming soon — pick 1 or 2.")
		}
	}
}

func chooseDialect(in *bufio.Scanner) string {
	fmt.Println("\nDatabase:")
	fmt.Println("  1) PostgreSQL (incl. Supabase / Neon)")
	fmt.Println("  2) MySQL / MariaDB (coming soon)")
	fmt.Println("  3) MongoDB (coming soon)")
	for {
		switch choose(in, "  choose [1]: ", 3, 1) {
		case 1:
			return "postgres"
		default:
			fmt.Println("  Only PostgreSQL is available in Phase 0 — pick 1.")
		}
	}
}

func buildDSN(dialect string, in *bufio.Scanner) string {
	// Phase 0 only has postgres; the menu enforces that.
	_ = dialect
	fmt.Println("\nConnection method:")
	fmt.Println("  1) Connection URI (postgresql://user:pass@host:5432/postgres)")
	fmt.Println("  2) Host / user / password fields")
	if choose(in, "  choose [1]: ", 2, 1) == 2 {
		host := promptDefault(in, "  host", "")
		port := promptDefault(in, "  port", "5432")
		db := promptDefault(in, "  database", "postgres")
		user := promptDefault(in, "  user", "querylynx_ro")
		pass := readSecret("  password: ")
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require",
			url.QueryEscape(user), url.QueryEscape(pass), host, port, db)
	}
	return prompt(in, "  paste connection URI: ")
}

func readAPIKey(provider string, in *bufio.Scanner) string {
	envVar := map[string]string{"anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY"}[provider]
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			fmt.Printf("  using API key from $%s\n", envVar)
			return v
		}
	}
	return readSecret("  API key: ")
}

// ----------------------------------------------------------------------------
// Ask loop
// ----------------------------------------------------------------------------

func askLoop(ctx context.Context, sess *session.Session, in *bufio.Scanner) {
	showTrace := false
	fmt.Println("\nAsk in plain English. Commands:  /trace   /tables   /exit")
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			return
		}
		q := strings.TrimSpace(in.Text())
		switch q {
		case "":
			continue
		case "/exit", "/quit":
			return
		case "/trace":
			showTrace = !showTrace
			fmt.Printf("trace: %s\n", onOff(showTrace))
			continue
		case "/tables":
			printSchema(sess.Schema)
			continue
		}

		ans, err := sess.Pipeline.Run(ctx, q)
		if err != nil {
			fmt.Printf("✗ %v\n", err)
			if ans != nil && ans.SQL != "" {
				fmt.Printf("  last SQL: %s\n", ans.SQL)
			}
			if showTrace && ans != nil {
				printTrace(ans.Explainability)
			}
			continue
		}
		fmt.Printf("\nSQL: %s\n", ans.SQL)
		if ans.Attempts > 1 {
			fmt.Printf("(succeeded after %d attempts)\n", ans.Attempts)
		}
		printTable(ans.Result)
		if showTrace {
			printTrace(ans.Explainability)
		}
	}
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

const maxDisplayRows = 50

func printTable(res connector.Result) {
	if len(res.Columns) == 0 {
		fmt.Println("(no columns returned)")
		return
	}
	widths := make([]int, len(res.Columns))
	for i, c := range res.Columns {
		widths[i] = len(c)
	}
	rows := make([][]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		cs := make([]string, len(res.Columns))
		for i := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			cs[i] = clip(cell(v), 40)
			if len(cs[i]) > widths[i] {
				widths[i] = len(cs[i])
			}
		}
		rows = append(rows, cs)
	}

	fmt.Println()
	printCells(res.Columns, widths)
	seps := make([]string, len(widths))
	for i, w := range widths {
		seps[i] = strings.Repeat("-", w)
	}
	printCells(seps, widths)

	shown := rows
	if len(shown) > maxDisplayRows {
		shown = shown[:maxDisplayRows]
	}
	for _, cs := range shown {
		printCells(cs, widths)
	}
	fmt.Printf("(%d rows", len(res.Rows))
	if len(rows) > maxDisplayRows {
		fmt.Printf(", showing first %d", maxDisplayRows)
	}
	fmt.Println(")")
}

func printCells(cells []string, widths []int) {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = fmt.Sprintf("%-*s", widths[i], c)
	}
	fmt.Println(strings.Join(parts, " | "))
}

func printTrace(ex agent.Explainability) {
	fmt.Printf("\ntrace %s\n", ex.TraceID)
	for _, s := range ex.Stages {
		status := s.Status
		if s.Error != "" {
			status = "error: " + s.Error
		}
		fmt.Printf("  %-11s %5dms  %s\n", s.Name, s.LatencyMS, status)
	}
}

func printSchema(s schema.Schema) {
	fmt.Printf("\nschema (%s):\n", s.DatabaseName)
	for _, t := range s.Tables {
		cols := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			cols[i] = c.Name
		}
		fmt.Printf("  %-24s (%d rows): %s\n", t.Name, t.RowCount, strings.Join(cols, ", "))
	}
}

// ----------------------------------------------------------------------------
// Small input helpers
// ----------------------------------------------------------------------------

func prompt(in *bufio.Scanner, label string) string {
	fmt.Print(label)
	if !in.Scan() {
		os.Exit(0)
	}
	return strings.TrimSpace(in.Text())
}

func promptDefault(in *bufio.Scanner, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	if !in.Scan() {
		os.Exit(0)
	}
	v := strings.TrimSpace(in.Text())
	if v == "" {
		return def
	}
	return v
}

func choose(in *bufio.Scanner, label string, max, def int) int {
	fmt.Print(label)
	if !in.Scan() {
		os.Exit(0)
	}
	v := strings.TrimSpace(in.Text())
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > max {
		return def
	}
	return n
}

func readSecret(label string) string {
	fmt.Print(label)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	// Non-terminal stdin (piped): fall back to a plain line read.
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func cell(v any) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprintf("%v", v)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

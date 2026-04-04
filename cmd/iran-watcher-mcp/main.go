package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

func main() {
	baseURL := envOrDefault("IRAN_WATCHER_MCP_BASE_URL", "http://localhost:8080")
	httpAddr := os.Getenv("IRAN_WATCHER_MCP_HTTP")

	// Simple flag parsing — no cobra/pflag dependency in this binary.
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base-url":
			if i+1 < len(args) {
				baseURL = args[i+1]
				i++
			}
		case "--http":
			if i+1 < len(args) {
				httpAddr = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Fprintln(os.Stdout, "iran-watcher-mcp — MCP server for iran-watcher REST API")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Usage:")
			fmt.Fprintln(os.Stdout, "  iran-watcher-mcp [flags]")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Flags:")
			fmt.Fprintln(os.Stdout, "  --base-url string   iran-watcher API base URL (default: http://localhost:8080)")
			fmt.Fprintln(os.Stdout, "                      Env: IRAN_WATCHER_MCP_BASE_URL")
			fmt.Fprintln(os.Stdout, "  --http string       Bind address for HTTP/SSE transport (e.g. :3001)")
			fmt.Fprintln(os.Stdout, "                      Env: IRAN_WATCHER_MCP_HTTP")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Default (no --http): serves MCP over stdio (for Claude Code, Cursor, etc.).")
			fmt.Fprintln(os.Stdout, "With --http: also starts an SSE server for programmatic clients.")
			os.Exit(0)
		}
	}

	srv := mcpserver.New(baseURL)
	ctx := context.Background()

	if httpAddr != "" {
		// SSE server runs in background; stdio runs in foreground.
		go func() {
			if err := srv.ServeHTTP(httpAddr); err != nil {
				fmt.Fprintf(os.Stderr, "iran-watcher-mcp: SSE server error: %v\n", err)
			}
		}()
	}

	if err := srv.ServeStdio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "iran-watcher-mcp: %v\n", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

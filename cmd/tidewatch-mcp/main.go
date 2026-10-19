package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amir-the-h/tidewatch/internal/mcpserver"
)

func main() {
	baseURL := envOrDefault("TIDEWATCH_MCP_BASE_URL", "http://localhost:8080")
	httpAddr := os.Getenv("TIDEWATCH_MCP_HTTP")
	username := os.Getenv("TIDEWATCH_MCP_USERNAME")
	password := os.Getenv("TIDEWATCH_MCP_PASSWORD")

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
		case "--username":
			if i+1 < len(args) {
				username = args[i+1]
				i++
			}
		case "--password":
			if i+1 < len(args) {
				password = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Fprintln(os.Stdout, "tidewatch-mcp — MCP server for tidewatch REST API")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Usage:")
			fmt.Fprintln(os.Stdout, "  tidewatch-mcp [flags]")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Flags:")
			fmt.Fprintln(os.Stdout, "  --base-url string   tidewatch API base URL (default: http://localhost:8080)")
			fmt.Fprintln(os.Stdout, "                      Env: TIDEWATCH_MCP_BASE_URL")
			fmt.Fprintln(os.Stdout, "  --http string       Bind address for HTTP/SSE transport (e.g. :3001)")
			fmt.Fprintln(os.Stdout, "                      Env: TIDEWATCH_MCP_HTTP")
			fmt.Fprintln(os.Stdout, "  --username string   tidewatch admin username for API auth")
			fmt.Fprintln(os.Stdout, "                      Env: TIDEWATCH_MCP_USERNAME")
			fmt.Fprintln(os.Stdout, "  --password string   tidewatch admin password for API auth")
			fmt.Fprintln(os.Stdout, "                      Env: TIDEWATCH_MCP_PASSWORD")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Default (no --http): serves MCP over stdio (for Claude Code, Cursor, etc.).")
			fmt.Fprintln(os.Stdout, "With --http: also starts an SSE server for programmatic clients.")
			os.Exit(0)
		}
	}

	srv := mcpserver.NewWithCredentials(baseURL, username, password)
	ctx := context.Background()

	if httpAddr != "" {
		// SSE server runs in background; stdio runs in foreground.
		go func() {
			if err := srv.ServeHTTP(httpAddr); err != nil {
				fmt.Fprintf(os.Stderr, "tidewatch-mcp: SSE server error: %v\n", err)
			}
		}()
	}

	if err := srv.ServeStdio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "tidewatch-mcp: %v\n", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

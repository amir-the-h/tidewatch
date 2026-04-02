# MCP Server Design

**Date:** 2026-04-02
**Status:** Approved

## Overview

A Model Context Protocol (MCP) server that exposes all iran-watcher REST API endpoints as MCP tools. Runs as a separate binary (`iran-watcher-mcp`) that talks to iran-watcher via HTTP. Supports both stdio (for Claude Code and desktop MCP clients) and HTTP/SSE (for the internal brain Ollama pipeline and programmatic clients) simultaneously.

## Goals

- Expose every iran-watcher REST API endpoint as an MCP tool
- Support Claude Code integration (stdio transport)
- Support the future internal brain Ollama pipeline (HTTP/SSE transport)
- Zero changes to the existing `iran-watcher` binary or packages
- No authentication required (trusted network / localhost assumed)

## Architecture

```
┌─────────────────────────┐        ┌─────────────────────────┐
│  Claude Code / external  │─stdio─▶│                         │
│  MCP client              │        │   iran-watcher-mcp      │
└─────────────────────────┘        │                         │──HTTP──▶ iran-watcher :8080
┌─────────────────────────┐        │   (single process)      │
│  Brain / internal client │─SSE──▶│                         │
└─────────────────────────┘        └─────────────────────────┘
```

The MCP server is a thin proxy: it receives MCP tool calls, translates them into HTTP requests against the iran-watcher REST API, and returns the response body as the tool result.

## File Structure

```
cmd/iran-watcher-mcp/
  main.go              — flag parsing, wires mcpserver, starts transport(s)

internal/mcpserver/
  server.go            — MCP server setup, registers all tools from table
  tools.go             — tool table (one ToolDef per endpoint)
  dispatcher.go        — generic HTTP dispatcher (table lookup → REST call → result)
  schema.go            — helpers for building JSON Schema objects from ParamDef slices
```

No changes to existing `internal/` packages or `cmd/iran-watcher/`.

## Data Types

### ToolDef

Each REST endpoint maps to one `ToolDef`:

```go
type ToolDef struct {
    Name         string      // e.g. "tweets_list", "pipelines_activate"
    Description  string      // shown to the LLM in tool listings
    Method       string      // "GET", "POST", "PUT", "DELETE"
    Path         string      // "/api/tweets/{id}" — {name} tokens for path params
    PathParams   []ParamDef  // always required; extracted from path template
    QueryParams  []ParamDef  // optional query string parameters
    Body         *BodyDef    // nil if the endpoint takes no request body
}

type ParamDef struct {
    Name        string   // matches {token} in path, or query key name
    Description string
    Required    bool
    Type        string   // "string" | "integer" | "boolean"
}

type BodyDef struct {
    Description string
    Schema      map[string]any  // raw JSON Schema object embedded as a field
}
```

### Tool Naming Convention

`{resource}_{verb}` — e.g.:
- `tweets_list`, `tweets_get`, `tweets_delete`
- `actions_create`, `actions_activate`, `actions_deactivate`
- `pipelines_get_graph`, `pipelines_save_graph`, `pipelines_run`
- `action_backends_set`, `entity_candidates_promote`
- `ollama_backends_enable`, `ollama_backends_disable`

## Tool Table (`tools.go`)

A `[]ToolDef` slice with one entry per endpoint, covering all ~50 routes registered in `internal/api/server.go`. The combined MCP input schema for each tool is built at server startup from `PathParams + QueryParams + Body` by `schema.go` helpers. No runtime reflection.

Example entries:

```go
{
    Name:        "tweets_list",
    Description: "List tweets with optional filtering by rule, source, date range, and text search.",
    Method:      "GET",
    Path:        "/api/tweets",
    QueryParams: []ParamDef{
        {Name: "rule_id", Type: "integer", Description: "Filter by rule ID"},
        {Name: "source_id", Type: "integer", Description: "Filter by source ID"},
        {Name: "q", Type: "string", Description: "Full-text search query"},
        {Name: "limit", Type: "integer", Description: "Max results to return"},
        {Name: "offset", Type: "integer", Description: "Pagination offset"},
    },
},
{
    Name:        "pipelines_activate",
    Description: "Activate a pipeline so it participates in tweet processing.",
    Method:      "POST",
    Path:        "/api/pipelines/{id}/activate",
    PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
},
{
    Name:        "actions_create",
    Description: "Create a new action (execution unit: onnx, ollama, internal, or notification type).",
    Method:      "POST",
    Path:        "/api/actions",
    Body: &BodyDef{
        Description: "Action definition",
        Schema: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "name":         map[string]any{"type": "string"},
                "type":         map[string]any{"type": "string", "enum": []string{"onnx", "ollama", "internal", "notification"}},
                "trigger_type": map[string]any{"type": "string"},
                "config":       map[string]any{"type": "object"},
            },
        },
    },
},
```

## Dispatcher (`dispatcher.go`)

```go
type Dispatcher struct {
    baseURL string
    client  *http.Client
    tools   map[string]ToolDef
}
```

For each tool call:

1. Look up `ToolDef` by tool name (error if not found)
2. Build URL path: substitute `{param}` tokens from tool call args
3. Append query string: include any `QueryParams` present in args
4. Build request body: if `Body != nil`, marshal the `body` arg field as JSON
5. Execute `http.Client.Do(req)`
6. Return response body as MCP text result

Non-2xx responses return the response body as an MCP error (passes iran-watcher error messages through to the LLM). Network failures return a descriptive MCP error string.

## Transport

Using `github.com/mark3labs/mcp-go`.

### stdio (default)

Active whenever the binary is run without `--http`. Reads MCP JSON-RPC from stdin, writes to stdout. Claude Code config:

```json
{
  "mcpServers": {
    "iran-watcher": {
      "command": "iran-watcher-mcp",
      "args": ["--base-url", "http://localhost:8080"]
    }
  }
}
```

### HTTP/SSE

Enabled by `--http :3001`. Binds an SSE endpoint at `http://<addr>/sse`. The internal brain pipeline or any HTTP MCP client connects here.

### Simultaneous

When `--http` is set, both stdio and SSE servers run in parallel goroutines within the same process. The same tool table and dispatcher instance is shared (dispatcher is stateless, safe for concurrent use).

## Configuration

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--base-url` | `IRAN_WATCHER_MCP_BASE_URL` | `http://localhost:8080` | iran-watcher API base URL |
| `--http` | `IRAN_WATCHER_MCP_HTTP` | `""` (disabled) | Address for HTTP/SSE server |

## Build

The binary is built by adding a second `COPY --from=builder` stage in the Dockerfile, mirroring how `iran-watcher` is built. No CGO required — the MCP binary is pure Go.

## Testing

- Unit tests for `dispatcher.go`: mock HTTP server validates path/query/body construction per ToolDef
- Unit tests for `schema.go`: validate JSON Schema output for various ParamDef combinations
- No integration tests required at this stage — the tool table is declarative data, not logic

## Out of Scope

- Authentication on the MCP server's own transports (no token required to call the MCP server)
- The "brain" Ollama pipeline itself — that is a separate future feature
- OpenAPI spec generation — considered for a future iteration

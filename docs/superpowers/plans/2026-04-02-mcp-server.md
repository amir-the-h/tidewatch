# MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `iran-watcher-mcp`, a separate binary that exposes all iran-watcher REST API endpoints as MCP tools, supporting both stdio and HTTP/SSE transports.

**Architecture:** Table-driven: each endpoint is declared as a `ToolDef` in `internal/mcpserver/tools.go`; a generic `Dispatcher` translates MCP tool calls into HTTP requests against the iran-watcher API (no auth — trusted network); `server.go` registers all tools and manages stdio/SSE transports using `mcp-go`.

**Tech Stack:** Go 1.25.5 (pure Go — no CGO), `github.com/mark3labs/mcp-go`, `net/http` (HTTP client + test helpers)

---

### Task 1: Add mcp-go dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add dependency**

Run from repo root:
```bash
go get github.com/mark3labs/mcp-go@latest
```
Expected: `go.mod` gains a `github.com/mark3labs/mcp-go` line.

- [ ] **Step 2: Tidy**

```bash
go mod tidy
```
Expected: `go.sum` updated, no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add mcp-go dependency"
```

---

### Task 2: Create types and schema builder

**Files:**
- Create: `internal/mcpserver/tools.go`
- Create: `internal/mcpserver/schema.go`
- Create: `internal/mcpserver/schema_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/mcpserver/schema_test.go`:
```go
package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestBuildToolOptions_PathParam(t *testing.T) {
	td := mcpserver.ToolDef{
		Name:        "test_get",
		Description: "Get something",
		Method:      "GET",
		Path:        "/api/test/{id}",
		PathParams: []mcpserver.ParamDef{
			{Name: "id", Type: "integer", Description: "Resource ID", Required: true},
		},
	}
	tool := mcp.NewTool(td.Name, mcpserver.BuildToolOptions(td)...)

	schema := tool.InputSchema
	if schema.Properties == nil {
		t.Fatal("expected non-nil properties")
	}
	if _, ok := schema.Properties["id"]; !ok {
		t.Error("expected 'id' property")
	}
	found := false
	for _, r := range schema.Required {
		if r == "id" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'id' to be in required list")
	}
}

func TestBuildToolOptions_QueryParams(t *testing.T) {
	td := mcpserver.ToolDef{
		Name:        "tweets_list",
		Description: "List tweets",
		Method:      "GET",
		Path:        "/api/tweets",
		QueryParams: []mcpserver.ParamDef{
			{Name: "rule_id", Type: "integer", Description: "Filter by rule"},
			{Name: "page", Type: "integer", Description: "Page number"},
		},
	}
	tool := mcp.NewTool(td.Name, mcpserver.BuildToolOptions(td)...)

	schema := tool.InputSchema
	if _, ok := schema.Properties["rule_id"]; !ok {
		t.Error("expected 'rule_id' property")
	}
	if _, ok := schema.Properties["page"]; !ok {
		t.Error("expected 'page' property")
	}
}

func TestBuildToolOptions_Body(t *testing.T) {
	td := mcpserver.ToolDef{
		Name:        "actions_create",
		Description: "Create action",
		Method:      "POST",
		Path:        "/api/actions",
		Body:        &mcpserver.BodyDef{Description: "Action definition"},
	}
	tool := mcp.NewTool(td.Name, mcpserver.BuildToolOptions(td)...)

	schema := tool.InputSchema
	if _, ok := schema.Properties["body"]; !ok {
		t.Error("expected 'body' property")
	}
}

func TestBuildToolOptions_NoParams(t *testing.T) {
	td := mcpserver.ToolDef{
		Name:        "health_get",
		Description: "Get health",
		Method:      "GET",
		Path:        "/api/health",
	}
	tool := mcp.NewTool(td.Name, mcpserver.BuildToolOptions(td)...)

	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("expected no required fields, got %v", tool.InputSchema.Required)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

```bash
go test ./internal/mcpserver/...
```
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Create `internal/mcpserver/tools.go`**

```go
package mcpserver

// ToolDef declares one REST endpoint as an MCP tool.
type ToolDef struct {
	Name        string
	Description string
	Method      string     // GET, POST, PUT, DELETE
	Path        string     // e.g. "/api/tweets/{id}"
	PathParams  []ParamDef // always required; substituted into path template
	QueryParams []ParamDef // optional query string fields
	Body        *BodyDef   // nil if endpoint takes no request body
}

// ParamDef describes a single parameter.
type ParamDef struct {
	Name        string
	Description string
	Required    bool
	Type        string // "string" | "integer" | "boolean"
}

// BodyDef describes a request body.
type BodyDef struct {
	Description string
}
```

- [ ] **Step 4: Create `internal/mcpserver/schema.go`**

```go
package mcpserver

import "github.com/mark3labs/mcp-go/mcp"

// BuildToolOptions converts a ToolDef into a slice of mcp.ToolOption
// ready to pass to mcp.NewTool.
func BuildToolOptions(td ToolDef) []mcp.ToolOption {
	opts := []mcp.ToolOption{mcp.WithDescription(td.Description)}

	for _, p := range td.PathParams {
		opts = append(opts, buildParamOption(p))
	}
	for _, p := range td.QueryParams {
		opts = append(opts, buildParamOption(p))
	}
	if td.Body != nil {
		var propOpts []mcp.PropertyOption
		if td.Body.Description != "" {
			propOpts = append(propOpts, mcp.Description(td.Body.Description))
		}
		opts = append(opts, mcp.WithObject("body", propOpts...))
	}
	return opts
}

func buildParamOption(p ParamDef) mcp.ToolOption {
	var propOpts []mcp.PropertyOption
	if p.Description != "" {
		propOpts = append(propOpts, mcp.Description(p.Description))
	}
	if p.Required {
		propOpts = append(propOpts, mcp.Required())
	}
	switch p.Type {
	case "integer":
		return mcp.WithNumber(p.Name, propOpts...)
	case "boolean":
		return mcp.WithBoolean(p.Name, propOpts...)
	default: // "string"
		return mcp.WithString(p.Name, propOpts...)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/mcpserver/...
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcpserver/tools.go internal/mcpserver/schema.go internal/mcpserver/schema_test.go
git commit -m "feat: add mcpserver types and schema builder"
```

---

### Task 3: Create dispatcher with tests

**Files:**
- Create: `internal/mcpserver/dispatcher.go`
- Create: `internal/mcpserver/dispatcher_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/mcpserver/dispatcher_test.go`:
```go
package mcpserver_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestDispatcher_GET_QueryParams(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tweets":[]}`))
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	td := mcpserver.ToolDef{
		Name:   "tweets_list",
		Method: "GET",
		Path:   "/api/tweets",
		QueryParams: []mcpserver.ParamDef{
			{Name: "rule_id", Type: "integer"},
			{Name: "page", Type: "integer"},
		},
	}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{
		"rule_id": float64(5),
		"page":    float64(2),
	}

	result, err := d.Handle(context.Background(), td, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result.Content)
	}
	if !strings.Contains(gotURL, "rule_id=5") {
		t.Errorf("expected rule_id=5 in URL, got %q", gotURL)
	}
	if !strings.Contains(gotURL, "page=2") {
		t.Errorf("expected page=2 in URL, got %q", gotURL)
	}
}

func TestDispatcher_POST_PathParam(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	td := mcpserver.ToolDef{
		Name:   "pipelines_activate",
		Method: "POST",
		Path:   "/api/pipelines/{id}/activate",
		PathParams: []mcpserver.ParamDef{
			{Name: "id", Type: "integer", Required: true},
		},
	}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"id": float64(42)}

	result, err := d.Handle(context.Background(), td, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result.Content)
	}
	if gotPath != "/api/pipelines/42/activate" {
		t.Errorf("expected /api/pipelines/42/activate, got %q", gotPath)
	}
}

func TestDispatcher_POST_Body(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	td := mcpserver.ToolDef{
		Name:   "actions_create",
		Method: "POST",
		Path:   "/api/actions",
		Body:   &mcpserver.BodyDef{Description: "Action definition"},
	}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{
		"body": map[string]any{"name": "my-action", "type": "ollama"},
	}

	result, err := d.Handle(context.Background(), td, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %v", result.Content)
	}
	if !strings.Contains(gotBody, "my-action") {
		t.Errorf("expected body to contain 'my-action', got %q", gotBody)
	}
}

func TestDispatcher_Non2xx_ReturnsErrorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	td := mcpserver.ToolDef{
		Name:   "actions_get",
		Method: "GET",
		Path:   "/api/actions/{id}",
		PathParams: []mcpserver.ParamDef{
			{Name: "id", Type: "integer", Required: true},
		},
	}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"id": float64(999)}

	result, err := d.Handle(context.Background(), td, req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for 404 response")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

```bash
go test ./internal/mcpserver/...
```
Expected: FAIL — `mcpserver.NewDispatcher` not defined.

- [ ] **Step 3: Implement dispatcher**

Create `internal/mcpserver/dispatcher.go`:
```go
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Dispatcher translates MCP tool calls into HTTP requests against the iran-watcher API.
type Dispatcher struct {
	baseURL string
	client  *http.Client
}

// NewDispatcher creates a Dispatcher that forwards tool calls to baseURL.
func NewDispatcher(baseURL string) *Dispatcher {
	return &Dispatcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

// Handle executes the REST call described by td with arguments from req.
func (d *Dispatcher) Handle(ctx context.Context, td ToolDef, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	if args == nil {
		args = map[string]any{}
	}

	// Build path — substitute {param} tokens.
	path := td.Path
	for _, p := range td.PathParams {
		val, ok := args[p.Name]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("missing required path param: %s", p.Name)), nil
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", fmt.Sprintf("%v", val))
	}

	// Build query string.
	q := url.Values{}
	for _, p := range td.QueryParams {
		if val, ok := args[p.Name]; ok {
			q.Set(p.Name, fmt.Sprintf("%v", val))
		}
	}
	rawURL := d.baseURL + path
	if len(q) > 0 {
		rawURL += "?" + q.Encode()
	}

	// Build body.
	var bodyReader io.Reader
	if td.Body != nil {
		if bodyVal, ok := args["body"]; ok {
			b, err := json.Marshal(bodyVal)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal body: %v", err)), nil
			}
			bodyReader = bytes.NewReader(b)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, td.Method, rawURL, bodyReader)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if bodyReader != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read response: %v", err)), nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(string(respBody)), nil
	}
	if len(respBody) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("OK (%d)", resp.StatusCode)), nil
	}
	return mcp.NewToolResultText(string(respBody)), nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/mcpserver/...
```
Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/dispatcher.go internal/mcpserver/dispatcher_test.go
git commit -m "feat: add mcpserver dispatcher with tests"
```

---

### Task 4: Build the tool table

**Files:**
- Modify: `internal/mcpserver/tools.go` (append `AllTools()`)

- [ ] **Step 1: Append `AllTools()` to `internal/mcpserver/tools.go`**

Append the following function to the existing file (do not replace existing types):

```go
// AllTools returns the complete ToolDef table for all iran-watcher REST endpoints.
// Source of truth: internal/api/server.go route registrations.
func AllTools() []ToolDef {
	return []ToolDef{

		// ── Auth ─────────────────────────────────────────────────────────────
		{
			Name:        "auth_me",
			Description: "Return the currently authenticated user's profile.",
			Method:      "GET",
			Path:        "/api/auth/me",
		},

		// ── Health / Stream ──────────────────────────────────────────────────
		{
			Name:        "health_get",
			Description: "Return API health status.",
			Method:      "GET",
			Path:        "/api/health",
		},
		{
			Name:        "stream_status_get",
			Description: "Return current X filtered stream connection status.",
			Method:      "GET",
			Path:        "/api/stream/status",
		},
		{
			Name:        "stream_connect",
			Description: "Connect to the X filtered stream (admin only).",
			Method:      "POST",
			Path:        "/api/stream/connect",
		},
		{
			Name:        "stream_disconnect",
			Description: "Disconnect from the X filtered stream (admin only).",
			Method:      "POST",
			Path:        "/api/stream/disconnect",
		},

		// ── Rules ────────────────────────────────────────────────────────────
		{
			Name:        "rules_list",
			Description: "List all stream filter rules.",
			Method:      "GET",
			Path:        "/api/rules",
		},
		{
			Name:        "rules_get",
			Description: "Get a single rule by ID.",
			Method:      "GET",
			Path:        "/api/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "rules_create",
			Description: "Create a new stream filter rule.",
			Method:      "POST",
			Path:        "/api/rules",
			Body: &BodyDef{
				Description: "Rule definition. Fields: name (string, required), expression (object, required), local (bool), active (bool).",
			},
		},
		{
			Name:        "rules_update",
			Description: "Update an existing rule.",
			Method:      "PUT",
			Path:        "/api/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
			Body: &BodyDef{
				Description: "Updated rule fields: name (string), expression (object), local (bool).",
			},
		},
		{
			Name:        "rules_delete",
			Description: "Delete a rule by ID.",
			Method:      "DELETE",
			Path:        "/api/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "rules_activate",
			Description: "Activate a rule (pushes to X filtered stream if non-local).",
			Method:      "POST",
			Path:        "/api/rules/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "rules_deactivate",
			Description: "Deactivate a rule.",
			Method:      "POST",
			Path:        "/api/rules/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},

		// ── Sources ──────────────────────────────────────────────────────────
		{
			Name:        "sources_list",
			Description: "List all tweet sources.",
			Method:      "GET",
			Path:        "/api/sources",
		},
		{
			Name:        "sources_create",
			Description: "Create a new source.",
			Method:      "POST",
			Path:        "/api/sources",
			Body: &BodyDef{
				Description: "Source definition. Fields: name (string, required), type (string), config (object).",
			},
		},
		{
			Name:        "sources_update",
			Description: "Update a source.",
			Method:      "PUT",
			Path:        "/api/sources/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
			Body: &BodyDef{
				Description: "Updated source fields: name (string), type (string), config (object).",
			},
		},
		{
			Name:        "sources_delete",
			Description: "Delete a source.",
			Method:      "DELETE",
			Path:        "/api/sources/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_deactivate",
			Description: "Deactivate a source.",
			Method:      "POST",
			Path:        "/api/sources/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},

		// ── Tweets ───────────────────────────────────────────────────────────
		{
			Name:        "tweets_list",
			Description: "List tweets with optional filtering. Returns paginated results with total count.",
			Method:      "GET",
			Path:        "/api/tweets",
			QueryParams: []ParamDef{
				{Name: "rule_id", Type: "integer", Description: "Filter by rule ID"},
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
				{Name: "page", Type: "integer", Description: "Page number (default 1)"},
				{Name: "per_page", Type: "integer", Description: "Results per page (default 20)"},
			},
		},
		{
			Name:        "tweets_get",
			Description: "Get a single tweet by ID including its action execution history and metadata.",
			Method:      "GET",
			Path:        "/api/tweets/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "string", Description: "Tweet ID (X tweet ID string)", Required: true}},
		},
		{
			Name:        "tweets_delete",
			Description: "Delete tweets by their IDs.",
			Method:      "DELETE",
			Path:        "/api/tweets",
			Body: &BodyDef{
				Description: "Object with field: ids (array of tweet ID strings, required).",
			},
		},
		{
			Name:        "tweets_reprocess",
			Description: "Re-queue tweets through the pipeline. Optionally targets specific action IDs.",
			Method:      "POST",
			Path:        "/api/tweets/reprocess",
			Body: &BodyDef{
				Description: "Object with fields: tweet_ids (array of strings, required), action_ids (array of integers, optional — targets specific actions).",
			},
		},
		{
			Name:        "tweets_noise_delete",
			Description: "Delete all tweets classified as noise.",
			Method:      "DELETE",
			Path:        "/api/tweets/noise",
		},

		// ── Map ──────────────────────────────────────────────────────────────
		{
			Name:        "map_pins_list",
			Description: "List geo-event map pins sourced from action_execution_metadata, with tweet_created_at for date-heat coloring.",
			Method:      "GET",
			Path:        "/api/map",
		},

		// ── Tracked Entities ─────────────────────────────────────────────────
		{
			Name:        "tracked_entities_list",
			Description: "List tracked entities. Optionally filter by type or active status.",
			Method:      "GET",
			Path:        "/api/tracked-entities",
			QueryParams: []ParamDef{
				{Name: "type", Type: "string", Description: "Filter by entity type (e.g. person, organization, location)"},
				{Name: "active", Type: "boolean", Description: "Filter by active status"},
			},
		},
		{
			Name:        "tracked_entities_get",
			Description: "Get a tracked entity by ID.",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_create",
			Description: "Create a new tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string), aliases (array of strings), notes (string).",
			},
		},
		{
			Name:        "tracked_entities_update",
			Description: "Update a tracked entity.",
			Method:      "PUT",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), type (string), aliases (array of strings), notes (string).",
			},
		},
		{
			Name:        "tracked_entities_delete",
			Description: "Delete a tracked entity by ID.",
			Method:      "DELETE",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_activate",
			Description: "Activate a tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_deactivate",
			Description: "Deactivate a tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_tracks_list",
			Description: "List NER track occurrences for a tracked entity (paginated).",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}/tracks",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
			QueryParams: []ParamDef{
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
			},
		},
		{
			Name:        "tracked_entities_alias_suggestions",
			Description: "List NER-derived alias suggestions for an existing tracked entity.",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}/alias-suggestions",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "alias_suggestions_search",
			Description: "Search NER alias suggestions by name and optional entity type (for new entity creation).",
			Method:      "GET",
			Path:        "/api/alias-suggestions",
			QueryParams: []ParamDef{
				{Name: "name", Type: "string", Description: "Name to search for"},
				{Name: "type", Type: "string", Description: "Entity type filter"},
			},
		},

		// ── Entity Candidates ────────────────────────────────────────────────
		{
			Name:        "entity_candidates_list",
			Description: "List auto-discovered entity candidates pending review.",
			Method:      "GET",
			Path:        "/api/entity-candidates",
		},
		{
			Name:        "entity_candidates_get",
			Description: "Get an entity candidate by ID.",
			Method:      "GET",
			Path:        "/api/entity-candidates/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_promote",
			Description: "Promote an entity candidate to a full tracked entity.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/promote",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_reject",
			Description: "Reject an entity candidate.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/reject",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_merge",
			Description: "Merge an entity candidate as an alias of an existing tracked entity.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/merge",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with field: entity_id (integer, the tracked entity to merge into).",
			},
		},
		{
			Name:        "entity_candidates_delete",
			Description: "Delete an entity candidate.",
			Method:      "DELETE",
			Path:        "/api/entity-candidates/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},

		// ── Entity Connections ───────────────────────────────────────────────
		{
			Name:        "entity_connections_list",
			Description: "List all entity co-occurrence connections with joined entity names.",
			Method:      "GET",
			Path:        "/api/entity-connections",
		},
		{
			Name:        "entity_connections_delete",
			Description: "Truncate all entity connections (admin only).",
			Method:      "DELETE",
			Path:        "/api/entity-connections",
		},
		{
			Name:        "entity_connections_subgraph",
			Description: "Get the BFS subgraph of entity connections centered on an entity.",
			Method:      "GET",
			Path:        "/api/entity-connections/subgraph",
			QueryParams: []ParamDef{
				{Name: "entity_id", Type: "integer", Description: "Center entity ID", Required: true},
				{Name: "depth", Type: "integer", Description: "BFS depth (default 2, max 5)"},
				{Name: "min_weight", Type: "integer", Description: "Minimum co-occurrence weight filter"},
			},
		},

		// ── Pipelines (DAG workflows) ────────────────────────────────────────
		{
			Name:        "pipelines_list",
			Description: "List all DAG pipeline workflows.",
			Method:      "GET",
			Path:        "/api/pipelines",
		},
		{
			Name:        "pipelines_get",
			Description: "Get a pipeline by ID.",
			Method:      "GET",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_create",
			Description: "Create a new DAG pipeline workflow.",
			Method:      "POST",
			Path:        "/api/pipelines",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), description (string).",
			},
		},
		{
			Name:        "pipelines_update",
			Description: "Update a pipeline's name or description.",
			Method:      "PUT",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), description (string).",
			},
		},
		{
			Name:        "pipelines_delete",
			Description: "Delete a pipeline.",
			Method:      "DELETE",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_activate",
			Description: "Activate a pipeline so it participates in tweet processing.",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_deactivate",
			Description: "Deactivate a pipeline.",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_graph_get",
			Description: "Get the DAG graph (nodes + edges) for a pipeline.",
			Method:      "GET",
			Path:        "/api/pipelines/{id}/graph",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_graph_save",
			Description: "Replace the full DAG graph for a pipeline. Detects and rejects cycles.",
			Method:      "PUT",
			Path:        "/api/pipelines/{id}/graph",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: nodes (array of PipelineNodeRequest), edges (array of PipelineEdgeRequest).",
			},
		},
		{
			Name:        "pipelines_run",
			Description: "Dispatch historical tweets through the pipeline's start node. Returns queued count.",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/run",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with field: new_only (bool, skip tweets already processed by this pipeline).",
			},
		},

		// ── Triggers ─────────────────────────────────────────────────────────
		{
			Name:        "triggers_list",
			Description: "List all available trigger types (new_tweet, new_categorization, entity_matched, etc.).",
			Method:      "GET",
			Path:        "/api/triggers",
		},
		{
			Name:        "triggers_links_list",
			Description: "List all trigger-to-pipeline links.",
			Method:      "GET",
			Path:        "/api/triggers/links",
		},
		{
			Name:        "triggers_links_create",
			Description: "Create a trigger-to-pipeline link (connects a trigger type to a pipeline entry node).",
			Method:      "POST",
			Path:        "/api/triggers/links",
			Body: &BodyDef{
				Description: "Object with fields: trigger_type (string, required), pipeline_id (integer, required).",
			},
		},
		{
			Name:        "triggers_links_delete",
			Description: "Delete a trigger-to-pipeline link.",
			Method:      "DELETE",
			Path:        "/api/triggers/links/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},
		{
			Name:        "triggers_links_activate",
			Description: "Activate a trigger-to-pipeline link.",
			Method:      "POST",
			Path:        "/api/triggers/links/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},
		{
			Name:        "triggers_links_deactivate",
			Description: "Deactivate a trigger-to-pipeline link.",
			Method:      "POST",
			Path:        "/api/triggers/links/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},

		// ── Actions ──────────────────────────────────────────────────────────
		{
			Name:        "actions_list",
			Description: "List actions (execution units). Optionally filter by active status or type.",
			Method:      "GET",
			Path:        "/api/actions",
			QueryParams: []ParamDef{
				{Name: "active", Type: "boolean", Description: "Filter by active status"},
				{Name: "type", Type: "string", Description: "Filter by type: onnx, ollama, internal, notification"},
			},
		},
		{
			Name:        "actions_get",
			Description: "Get an action by ID.",
			Method:      "GET",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_create",
			Description: "Create a new action. type must be: onnx, ollama, internal, or notification.",
			Method:      "POST",
			Path:        "/api/actions",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string, required: onnx|ollama|internal|notification), trigger_type (string), config (object, pipeline-specific JSON).",
			},
		},
		{
			Name:        "actions_update",
			Description: "Update an action's name, type, or config.",
			Method:      "PUT",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), type (string), config (object).",
			},
		},
		{
			Name:        "actions_delete",
			Description: "Delete an action.",
			Method:      "DELETE",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_activate",
			Description: "Activate an action so it processes incoming triggers.",
			Method:      "POST",
			Path:        "/api/actions/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_deactivate",
			Description: "Deactivate an action.",
			Method:      "POST",
			Path:        "/api/actions/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_handlers_list",
			Description: "List registered internal action handler names (entity_matcher, twitter_retweet, etc.).",
			Method:      "GET",
			Path:        "/api/actions/handlers",
		},
		{
			Name:        "models_list",
			Description: "List available ONNX model files in the models directory.",
			Method:      "GET",
			Path:        "/api/models",
		},

		// ── Action Executions ────────────────────────────────────────────────
		{
			Name:        "action_executions_list",
			Description: "List execution history for a specific action (paginated).",
			Method:      "GET",
			Path:        "/api/actions/{id}/executions",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
			},
		},
		{
			Name:        "executions_list",
			Description: "List all action executions across all actions (paginated).",
			Method:      "GET",
			Path:        "/api/executions",
			QueryParams: []ParamDef{
				{Name: "tweet_id", Type: "string", Description: "Filter by tweet ID"},
				{Name: "status", Type: "string", Description: "Filter by status (success, error)"},
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
			},
		},
		{
			Name:        "execution_metadata_get",
			Description: "Get the key-value metadata map for a single action execution.",
			Method:      "GET",
			Path:        "/api/executions/{id}/metadata",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Execution ID", Required: true}},
		},
		{
			Name:        "executions_delete",
			Description: "Delete executions for specified tweet IDs, optionally filtered to specific action IDs.",
			Method:      "DELETE",
			Path:        "/api/executions",
			Body: &BodyDef{
				Description: "Object with fields: tweet_ids (array of strings, required), action_ids (array of integers, optional).",
			},
		},

		// ── Action Backends ──────────────────────────────────────────────────
		{
			Name:        "action_backends_list",
			Description: "List Ollama backend priority assignments for an action.",
			Method:      "GET",
			Path:        "/api/actions/{id}/backends",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "action_backends_set",
			Description: "Set (replace) Ollama backend priority assignments for an action.",
			Method:      "PUT",
			Path:        "/api/actions/{id}/backends",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			Body: &BodyDef{
				Description: "Array of objects: [{backend_id: integer, priority: integer}]. Replaces all existing assignments.",
			},
		},
		{
			Name:        "action_backends_remove",
			Description: "Remove a specific Ollama backend assignment from an action.",
			Method:      "DELETE",
			Path:        "/api/actions/{id}/backends/{backend_id}",
			PathParams: []ParamDef{
				{Name: "id", Type: "integer", Description: "Action ID", Required: true},
				{Name: "backend_id", Type: "integer", Description: "Backend ID", Required: true},
			},
		},

		// ── Ollama Backends ──────────────────────────────────────────────────
		{
			Name:        "ollama_backends_list",
			Description: "List Ollama backends merged with live health and scoring stats.",
			Method:      "GET",
			Path:        "/api/ollama/backends",
		},
		{
			Name:        "ollama_backends_create",
			Description: "Register a new Ollama backend.",
			Method:      "POST",
			Path:        "/api/ollama/backends",
			Body: &BodyDef{
				Description: "Object with fields: name (string), url (string, required, e.g. http://localhost:11434).",
			},
		},
		{
			Name:        "ollama_backends_update",
			Description: "Update an Ollama backend's name or URL.",
			Method:      "PUT",
			Path:        "/api/ollama/backends/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), url (string).",
			},
		},
		{
			Name:        "ollama_backends_delete",
			Description: "Delete an Ollama backend.",
			Method:      "DELETE",
			Path:        "/api/ollama/backends/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},
		{
			Name:        "ollama_backends_enable",
			Description: "Enable an Ollama backend (makes it eligible for task assignment).",
			Method:      "POST",
			Path:        "/api/ollama/backends/{id}/enable",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},
		{
			Name:        "ollama_backends_disable",
			Description: "Disable an Ollama backend.",
			Method:      "POST",
			Path:        "/api/ollama/backends/{id}/disable",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},

		// ── Queue ────────────────────────────────────────────────────────────
		{
			Name:        "queue_get",
			Description: "Get per-action queue depths.",
			Method:      "GET",
			Path:        "/api/queue",
		},
		{
			Name:        "queue_flush",
			Description: "Flush all pending task queues, removing all queued tasks.",
			Method:      "DELETE",
			Path:        "/api/queue",
		},
		{
			Name:        "workers_buffer_flush",
			Description: "Flush the in-memory worker buffer for all pipelines.",
			Method:      "DELETE",
			Path:        "/api/workers/buffer",
		},
		{
			Name:        "workers_buffer_flush_pipeline",
			Description: "Flush the in-memory worker buffer for a specific pipeline name.",
			Method:      "DELETE",
			Path:        "/api/workers/buffer/{pipeline}",
			PathParams:  []ParamDef{{Name: "pipeline", Type: "string", Description: "Pipeline name", Required: true}},
		},

		// ── Search Jobs ──────────────────────────────────────────────────────
		{
			Name:        "search_jobs_list",
			Description: "List X search backfill jobs.",
			Method:      "GET",
			Path:        "/api/search",
		},
		{
			Name:        "search_run",
			Description: "Trigger an X search backfill job. Returns 202 with job ID immediately; poll search_job_get until done or error.",
			Method:      "POST",
			Path:        "/api/search/run",
			Body: &BodyDef{
				Description: "Optional object with fields: rule_ids (array of integers), query (string), start_time (RFC3339), end_time (RFC3339).",
			},
		},
		{
			Name:        "search_job_get",
			Description: "Get a search job by ID. Poll until status is done or error.",
			Method:      "GET",
			Path:        "/api/search/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Search job ID", Required: true}},
		},
		{
			Name:        "search_job_delete",
			Description: "Delete a search job.",
			Method:      "DELETE",
			Path:        "/api/search/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Search job ID", Required: true}},
		},

		// ── Summaries ────────────────────────────────────────────────────────
		{
			Name:        "summaries_list",
			Description: "List generated summaries.",
			Method:      "GET",
			Path:        "/api/summaries",
		},
		{
			Name:        "summaries_run",
			Description: "Trigger summary generation.",
			Method:      "POST",
			Path:        "/api/summaries/run",
		},

		// ── Digest Jobs ──────────────────────────────────────────────────────
		{
			Name:        "digest_list",
			Description: "List LLM digest jobs.",
			Method:      "GET",
			Path:        "/api/digest",
		},
		{
			Name:        "digest_create",
			Description: "Create and dispatch a new LLM digest job over a tweet window. Returns 202.",
			Method:      "POST",
			Path:        "/api/digest",
			Body: &BodyDef{
				Description: "Optional object with fields: from (RFC3339), to (RFC3339), pipeline_id (integer).",
			},
		},
		{
			Name:        "digest_get",
			Description: "Get a digest job by ID. Poll until status is done or error.",
			Method:      "GET",
			Path:        "/api/digest/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Digest job ID", Required: true}},
		},
		{
			Name:        "digest_delete",
			Description: "Delete a digest job.",
			Method:      "DELETE",
			Path:        "/api/digest/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Digest job ID", Required: true}},
		},

		// ── Chat ─────────────────────────────────────────────────────────────
		{
			Name:        "chat_estimate",
			Description: "Estimate token count for a set of tweet IDs against the chat model's context window.",
			Method:      "POST",
			Path:        "/api/chat/estimate",
			Body: &BodyDef{
				Description: "Object with field: tweet_ids (array of tweet ID strings).",
			},
		},
		{
			Name:        "chat_stream",
			Description: "Stream an LLM chat response grounded in a set of tweets. Returns raw SSE chunks.",
			Method:      "POST",
			Path:        "/api/chat/stream",
			Body: &BodyDef{
				Description: "Object with fields: tweet_ids (array of strings), messages (array of {role: string, content: string}).",
			},
		},

		// ── Failures (dead-letter queue) ─────────────────────────────────────
		{
			Name:        "failures_list",
			Description: "List tasks in the dead-letter queue.",
			Method:      "GET",
			Path:        "/api/failures",
		},
		{
			Name:        "failures_clear",
			Description: "Clear all tasks from the dead-letter queue.",
			Method:      "DELETE",
			Path:        "/api/failures",
		},

		// ── Notification Providers ───────────────────────────────────────────
		{
			Name:        "notification_providers_list",
			Description: "List notification providers (Pushover, Telegram).",
			Method:      "GET",
			Path:        "/api/notification-providers",
		},
		{
			Name:        "notification_providers_create",
			Description: "Create a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string: pushover|telegram), config (object: pushover needs {app_token, user_key}; telegram needs {bot_token}).",
			},
		},
		{
			Name:        "notification_providers_get",
			Description: "Get a notification provider by ID.",
			Method:      "GET",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_update",
			Description: "Update a notification provider.",
			Method:      "PUT",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), config (object).",
			},
		},
		{
			Name:        "notification_providers_delete",
			Description: "Delete a notification provider.",
			Method:      "DELETE",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_activate",
			Description: "Activate a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_deactivate",
			Description: "Deactivate a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},

		// ── Notification Channels ────────────────────────────────────────────
		{
			Name:        "notification_channels_list",
			Description: "List notification channels.",
			Method:      "GET",
			Path:        "/api/notification-channels",
		},
		{
			Name:        "notification_channels_create",
			Description: "Create a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), provider_id (integer, required), message_template (string, supports ${key} substitution), config (object, e.g. {chat_id: string} for Telegram).",
			},
		},
		{
			Name:        "notification_channels_get",
			Description: "Get a notification channel by ID.",
			Method:      "GET",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_update",
			Description: "Update a notification channel.",
			Method:      "PUT",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), provider_id (integer), message_template (string), config (object).",
			},
		},
		{
			Name:        "notification_channels_delete",
			Description: "Delete a notification channel.",
			Method:      "DELETE",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_activate",
			Description: "Activate a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_deactivate",
			Description: "Deactivate a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},

		// ── Users ────────────────────────────────────────────────────────────
		{
			Name:        "users_list",
			Description: "List all users (admin only).",
			Method:      "GET",
			Path:        "/api/users",
		},
		{
			Name:        "users_create",
			Description: "Create a new user (admin only).",
			Method:      "POST",
			Path:        "/api/users",
			Body: &BodyDef{
				Description: "Object with fields: username (string, required), password (string, required), role (string: admin|viewer).",
			},
		},
		{
			Name:        "users_update",
			Description: "Update a user's username, password, or role (admin only).",
			Method:      "PUT",
			Path:        "/api/users/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "User ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: username (string), password (string), role (string).",
			},
		},
		{
			Name:        "users_delete",
			Description: "Delete a user (admin only).",
			Method:      "DELETE",
			Path:        "/api/users/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "User ID", Required: true}},
		},

		// ── Twitter Auth ─────────────────────────────────────────────────────
		{
			Name:        "twitter_auth_start",
			Description: "Get the OAuth 2.0 PKCE authorization URL to connect a Twitter account.",
			Method:      "GET",
			Path:        "/api/twitter/auth/start",
		},
		{
			Name:        "twitter_auth_status",
			Description: "Get the current Twitter OAuth connection status.",
			Method:      "GET",
			Path:        "/api/twitter/auth/status",
		},
		{
			Name:        "twitter_auth_delete",
			Description: "Disconnect the linked Twitter account.",
			Method:      "DELETE",
			Path:        "/api/twitter/auth",
		},
	}
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/mcpserver/...
```
Expected: no errors.

- [ ] **Step 3: Run tests**

```bash
go test ./internal/mcpserver/...
```
Expected: PASS (existing tests unaffected).

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/tools.go
git commit -m "feat: add full MCP tool table for all iran-watcher endpoints"
```

---

### Task 5: Create the MCP server wrapper

**Files:**
- Create: `internal/mcpserver/server.go`

- [ ] **Step 1: Create `internal/mcpserver/server.go`**

```go
package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go server and dispatcher.
type Server struct {
	mcp        *server.MCPServer
	dispatcher *Dispatcher
}

// New creates a Server that proxies all iran-watcher endpoints to baseURL.
func New(baseURL string) *Server {
	d := NewDispatcher(baseURL)

	s := server.NewMCPServer("iran-watcher", "1.0.0")

	for _, td := range AllTools() {
		td := td // capture loop variable for closure
		tool := mcp.NewTool(td.Name, BuildToolOptions(td)...)
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return d.Handle(ctx, td, req)
		})
	}

	return &Server{mcp: s, dispatcher: d}
}

// ServeStdio serves the MCP protocol over stdin/stdout. Blocks until the
// client disconnects or an error occurs.
func (s *Server) ServeStdio(_ context.Context) error {
	return server.ServeStdio(s.mcp)
}

// ServeHTTP starts an SSE server on addr (e.g. ":3001"). Blocks until stopped.
func (s *Server) ServeHTTP(addr string) error {
	sseServer := server.NewSSEServer(s.mcp,
		server.WithBaseURL(fmt.Sprintf("http://%s", addr)),
	)
	return sseServer.Start(addr)
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/mcpserver/...
```
Expected: no errors.

- [ ] **Step 3: Run all mcpserver tests**

```bash
go test ./internal/mcpserver/...
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/server.go
git commit -m "feat: add mcpserver.Server with stdio and SSE transport"
```

---

### Task 6: Create the binary

**Files:**
- Create: `cmd/iran-watcher-mcp/main.go`

- [ ] **Step 1: Create `cmd/iran-watcher-mcp/main.go`**

```go
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/iran-watcher-mcp/...
```
Expected: produces binary `iran-watcher-mcp` with no errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/iran-watcher-mcp/main.go
git commit -m "feat: add iran-watcher-mcp binary entrypoint"
```

---

### Task 7: Update Dockerfile and verify build

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Add MCP binary build step**

In `Dockerfile`, locate:
```dockerfile
RUN CGO_ENABLED=1 GOOS=linux \
    CGO_LDFLAGS="-L/usr/local/lib" \
    go build -ldflags="-s -w" -o iran-watcher ./cmd/iran-watcher
```

Add a second build line directly after it:
```dockerfile
RUN CGO_ENABLED=1 GOOS=linux \
    CGO_LDFLAGS="-L/usr/local/lib" \
    go build -ldflags="-s -w" -o iran-watcher ./cmd/iran-watcher

RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -o iran-watcher-mcp ./cmd/iran-watcher-mcp
```

- [ ] **Step 2: Copy MCP binary into runtime stage**

In `Dockerfile`, locate:
```dockerfile
COPY --from=builder /app/iran-watcher /usr/local/bin/iran-watcher
```

Add the MCP binary alongside it:
```dockerfile
COPY --from=builder /app/iran-watcher /usr/local/bin/iran-watcher
COPY --from=builder /app/iran-watcher-mcp /usr/local/bin/iran-watcher-mcp
```

- [ ] **Step 3: Build to verify**

```bash
docker compose build iran-watcher
```
Expected: image builds successfully; no errors. Both `iran-watcher` and `iran-watcher-mcp` are present in `/usr/local/bin/`.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "feat: add iran-watcher-mcp binary to Docker image"
```

---

### Task 8: Update docs

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add to Project Structure section**

In the Project Structure code block, add:
```
cmd/iran-watcher-mcp/   → MCP server binary entrypoint (pure Go, no CGO)
internal/mcpserver/     → MCP server: table-driven ToolDef registry, HTTP Dispatcher, stdio/SSE transports
```

- [ ] **Step 2: Add to Key Design Decisions section**

Add a new bullet in Key Design Decisions:
```
- **MCP server** — `iran-watcher-mcp` is a separate pure-Go binary that exposes all REST API endpoints as MCP tools; table-driven: each endpoint is a `ToolDef` in `internal/mcpserver/tools.go`; a `Dispatcher` translates tool calls into HTTP requests against iran-watcher with no auth (trusted network assumed); supports stdio (for Claude Code and desktop MCP clients) and HTTP/SSE (for programmatic clients and the future brain pipeline) — both transports can run simultaneously; configured via `--base-url` (default `http://localhost:8080`) and `--http` (optional SSE bind address); env vars: `IRAN_WATCHER_MCP_BASE_URL`, `IRAN_WATCHER_MCP_HTTP`; built alongside the main binary in the same Dockerfile stage
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document MCP server in CLAUDE.md"
```

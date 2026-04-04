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
	if len(result.Content) == 0 {
		t.Error("expected non-empty content for 204 response")
	}
}

func TestDispatcher_MissingPathParam_ReturnsError(t *testing.T) {
	d := mcpserver.NewDispatcher("http://localhost:9999") // won't be called
	td := mcpserver.ToolDef{
		Name:   "actions_get",
		Method: "GET",
		Path:   "/api/actions/{id}",
		PathParams: []mcpserver.ParamDef{
			{Name: "id", Type: "integer", Required: true},
		},
	}

	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{} // id is missing

	result, err := d.Handle(context.Background(), td, req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing path param")
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

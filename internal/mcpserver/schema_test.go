package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/tidewatch/internal/mcpserver"
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

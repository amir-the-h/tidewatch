package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

func TestBuildOllamaTools_EmptyAllowed_ReturnsAll(t *testing.T) {
	tools := mcpserver.BuildOllamaTools(nil)
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list")
	}
	// All 130+ tools should be present — just verify count is large
	if len(tools) < 50 {
		t.Errorf("expected at least 50 tools, got %d", len(tools))
	}
}

func TestBuildOllamaTools_FilteredAllowed(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_list", "health_get"})
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Function.Name] = true
	}
	if !names["tweets_list"] {
		t.Error("expected tweets_list in filtered tools")
	}
	if !names["health_get"] {
		t.Error("expected health_get in filtered tools")
	}
}

func TestBuildOllamaTools_FilteredUnknownName_Ignored(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_list", "does_not_exist"})
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (unknown ignored), got %d", len(tools))
	}
}

func TestBuildOllamaTools_PathParamMapped(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_get"})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	props, ok := tool.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters.properties is not a map")
	}
	idProp, ok := props["id"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'id' property")
	}
	if idProp["type"] != "string" {
		t.Errorf("expected type=string for id, got %v", idProp["type"])
	}
	req, ok := tool.Function.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}
	var hasID bool
	for _, r := range req {
		if r == "id" {
			hasID = true
		}
	}
	if !hasID {
		t.Error("expected 'id' in required array")
	}
}

func TestBuildOllamaTools_BodyMapped(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"actions_create"})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	props, ok := tools[0].Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters.properties is not a map")
	}
	if _, ok := props["body"]; !ok {
		t.Error("expected 'body' property for actions_create")
	}
}

func TestBuildOllamaTools_TypeField(t *testing.T) {
	tools := mcpserver.BuildOllamaTools(nil)
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s: expected type=function, got %q", tool.Function.Name, tool.Type)
		}
	}
}

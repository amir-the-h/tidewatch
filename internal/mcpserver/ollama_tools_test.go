package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

func TestBuildOllamaTools_Count(t *testing.T) {
	tools := mcpserver.BuildOllamaTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list")
	}
}

func TestBuildOllamaTools_PathParamMapped(t *testing.T) {
	// Find tweets_get which has a required path param "id"
	tools := mcpserver.BuildOllamaTools()
	var found bool
	for _, tool := range tools {
		if tool.Function.Name != "tweets_get" {
			continue
		}
		found = true
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
	if !found {
		t.Fatal("tweets_get tool not found")
	}
}

func TestBuildOllamaTools_BodyMapped(t *testing.T) {
	// Find actions_create which has a body
	tools := mcpserver.BuildOllamaTools()
	for _, tool := range tools {
		if tool.Function.Name != "actions_create" {
			continue
		}
		props, ok := tool.Function.Parameters["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("parameters.properties is not a map")
		}
		if _, ok := props["body"]; !ok {
			t.Error("expected 'body' property for actions_create")
		}
		return
	}
	t.Fatal("actions_create tool not found")
}

func TestBuildOllamaTools_TypeField(t *testing.T) {
	tools := mcpserver.BuildOllamaTools()
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s: expected type=function, got %q", tool.Function.Name, tool.Type)
		}
	}
}

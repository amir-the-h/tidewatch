package mcpserver_test

import (
	"strings"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/mcpserver"
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

func TestBuildOllamaTools_EmptySlice_ReturnsAll(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{})
	if len(tools) < 50 {
		t.Errorf("expected at least 50 tools for empty slice, got %d", len(tools))
	}
}

func TestBuildOllamaTools_FilteredAllowed(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"signals_list", "health_get"})
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Function.Name] = true
	}
	if !names["signals_list"] {
		t.Error("expected signals_list in filtered tools")
	}
	if !names["health_get"] {
		t.Error("expected health_get in filtered tools")
	}
}

func TestBuildOllamaTools_FilteredUnknownName_Ignored(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"signals_list", "does_not_exist"})
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (unknown ignored), got %d", len(tools))
	}
}

func TestBuildOllamaTools_PathParamMapped(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"signals_get"})
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
	if idProp["type"] != "integer" {
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

// TestAllTools_SignalRename checks the tweet tools were renamed to signals_*
// and no tool still targets a /api/tweets route.
func TestAllTools_SignalRename(t *testing.T) {
	byName := map[string]mcpserver.ToolDef{}
	for _, td := range mcpserver.AllTools() {
		byName[td.Name] = td
		if strings.HasPrefix(td.Name, "tweets_") || strings.HasPrefix(td.Path, "/api/tweets") {
			t.Errorf("tool %s (%s) still uses the tweets name or route", td.Name, td.Path)
		}
	}
	for _, name := range []string{"signals_list", "signals_get", "signals_delete", "signals_reprocess", "signals_noise_delete"} {
		td, ok := byName[name]
		if !ok {
			t.Errorf("missing tool %s", name)
			continue
		}
		if !strings.HasPrefix(td.Path, "/api/signals") {
			t.Errorf("tool %s: path %s, want /api/signals…", name, td.Path)
		}
	}
}

// TestAllTools_Sources checks the X rule builder tools moved to x_terms_* /
// x_rules_* under /api/x, and the generic source tools exist.
func TestAllTools_Sources(t *testing.T) {
	byName := map[string]mcpserver.ToolDef{}
	for _, td := range mcpserver.AllTools() {
		byName[td.Name] = td
		if strings.HasPrefix(td.Name, "rules_") || strings.HasPrefix(td.Path, "/api/rules") {
			t.Errorf("tool %s (%s) still uses the old rules name or route", td.Name, td.Path)
		}
		if strings.HasPrefix(td.Name, "x_") && !strings.HasPrefix(td.Path, "/api/x/") {
			t.Errorf("tool %s: path %s, want /api/x/…", td.Name, td.Path)
		}
		if (strings.HasPrefix(td.Name, "sources_") || strings.HasPrefix(td.Name, "source_")) &&
			!strings.HasPrefix(td.Path, "/api/sources") && !strings.HasPrefix(td.Path, "/api/source-runs/") {
			t.Errorf("tool %s: path %s, want /api/sources… or /api/source-runs/…", td.Name, td.Path)
		}
	}
	for _, name := range []string{
		"x_terms_list", "x_terms_create", "x_terms_update", "x_terms_delete", "x_terms_bulk_delete", "x_terms_deactivate",
		"x_rules_list", "x_rules_get", "x_rules_create", "x_rules_update", "x_rules_delete", "x_rules_bulk_delete",
		"x_rules_activate", "x_rules_deactivate",
		"sources_list", "sources_get", "sources_create", "sources_update", "sources_delete", "sources_bulk_delete",
		"sources_activate", "sources_deactivate", "sources_fetch", "sources_backfill", "sources_status_get",
		"source_types_list", "source_runs_list", "source_runs_get",
	} {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing tool %s", name)
		}
	}
}

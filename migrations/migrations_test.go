package migrations

import (
	"strings"
	"testing"
)

func TestFSReadFile(t *testing.T) {
	content, err := FS.ReadFile("001_schema.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("SQL content is empty")
	}
}

func TestSQLContainsAllTables(t *testing.T) {
	content, err := FS.ReadFile("001_schema.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}

	sql := string(content)
	tables := []string{"signals", "signal_sources", "x_rule_matches", "action_executions", "entity_tracks"}

	for _, table := range tables {
		if !strings.Contains(sql, "CREATE TABLE "+table+" (") {
			t.Errorf("SQL does not contain table: %s", table)
		}
	}
	for _, gone := range []string{"tweets", "tweet_rules", "categorizations", "chains"} {
		if strings.Contains(sql, "CREATE TABLE "+gone+" (") {
			t.Errorf("SQL still creates dead table: %s", gone)
		}
	}
}

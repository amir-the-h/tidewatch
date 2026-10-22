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

func TestSourcesMigration(t *testing.T) {
	content, err := FS.ReadFile("002_sources.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}
	sql := string(content)
	for _, want := range []string{
		"ALTER TABLE sources RENAME TO x_terms",
		"ALTER TABLE rules RENAME TO x_rules",
		"CREATE TABLE sources (",
		"CREATE TABLE source_state (",
		"CREATE TABLE source_runs (",
		"CREATE TABLE signal_sources (",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("002_sources.sql lacks %q", want)
		}
	}
	// The renames must run before the new sources table exists.
	if strings.Index(sql, "RENAME TO x_terms") > strings.Index(sql, "CREATE TABLE sources (") {
		t.Error("sources is created before the rule-atom table is renamed away")
	}
}

func TestDropSearchJobsMigration(t *testing.T) {
	content, err := FS.ReadFile("003_drop_search_jobs.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}
	if !strings.Contains(string(content), "DROP TABLE search_jobs") {
		t.Error("003_drop_search_jobs.sql doesn't drop search_jobs")
	}
}

package migrations

import (
	"strings"
	"testing"
)

func TestFSReadFile(t *testing.T) {
	content, err := FS.ReadFile("001_initial.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("SQL content is empty")
	}
}

func TestSQLContainsAllTables(t *testing.T) {
	content, err := FS.ReadFile("001_initial.sql")
	if err != nil {
		t.Fatalf("FS.ReadFile failed: %v", err)
	}

	sql := string(content)
	tables := []string{"sources", "tweets", "tweet_sources", "categorizations", "models"}

	for _, table := range tables {
		if !strings.Contains(sql, table) {
			t.Errorf("SQL does not contain table: %s", table)
		}
	}
}

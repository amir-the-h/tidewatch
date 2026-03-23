package store

import (
	"bufio"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// RunMigrations creates a schema_migrations table (if not exists), then applies
// all .sql files from migrationsFS in lexicographic order, skipping files
// already recorded as applied.
func RunMigrations(db *sql.DB, migrationsFS fs.FS) error {
	// 1. Create migrations tracking table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("store: migrations: create tracking table: %w", err)
	}

	// 2. Read all *.sql files from FS, sort by filename
	entries, err := fs.Glob(migrationsFS, "*.sql")
	if err != nil {
		return fmt.Errorf("store: migrations: glob sql files: %w", err)
	}
	sort.Strings(entries)

	// 3. For each file: check if already applied, if not: execute SQL, record as applied
	for _, filename := range entries {
		var count int
		err = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, filename).Scan(&count)
		if err != nil {
			return fmt.Errorf("store: migrations: check applied %s: %w", filename, err)
		}
		if count > 0 {
			continue
		}

		data, err := fs.ReadFile(migrationsFS, filename)
		if err != nil {
			return fmt.Errorf("store: migrations: read %s: %w", filename, err)
		}

		// Split on ";" and execute statements individually (modernc.org/sqlite does
		// not support multi-statement Exec). Strip line comments before splitting
		// to avoid false splits on semicolons inside -- comment lines.
		stripped := stripLineComments(string(data))
		statements := strings.Split(stripped, ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("store: migrations: exec %s: %w", filename, err)
			}
		}

		if _, err := db.Exec(`INSERT INTO schema_migrations (filename) VALUES (?)`, filename); err != nil {
			return fmt.Errorf("store: migrations: record %s: %w", filename, err)
		}
	}

	return nil
}

// stripLineComments removes SQL -- line comments from s so that semicolons
// embedded in comments do not confuse the statement splitter.
func stripLineComments(s string) string {
	var b strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

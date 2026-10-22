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

	// 3. Refuse a database whose applied migrations are not embedded any more.
	// Phase 1 replaced migrations 001–029 with a single 001_schema.sql and the
	// old database is not upgraded.
	if err := checkUnknownMigrations(db, entries); err != nil {
		return err
	}

	// 4. For each file: check if already applied, if not: execute SQL, record as applied
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

// checkUnknownMigrations returns an error when schema_migrations records a
// file that is not among the embedded migrations, which means the database was
// created by an older, incompatible schema.
func checkUnknownMigrations(db *sql.DB, known []string) error {
	knownSet := make(map[string]bool, len(known))
	for _, f := range known {
		knownSet[f] = true
	}
	rows, err := db.Query(`SELECT filename FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("store: migrations: list applied: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return fmt.Errorf("store: migrations: scan applied: %w", err)
		}
		if !knownSet[f] {
			return fmt.Errorf("store: migrations: database was created by an older schema (applied migration %s is unknown); "+
				"the phase 1 schema reset does not upgrade it — move the SQLite file aside and restart", f)
		}
	}
	return rows.Err()
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

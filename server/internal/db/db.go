// Package db owns the pgx connection pool and the embedded-SQL migration runner.
//
// Migrations are embedded in the binary (embed.FS) and applied at startup one
// statement at a time in autocommit mode. This is mandatory because TimescaleDB
// continuous aggregates (CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous))
// cannot be created inside a transaction block. Applied files are tracked in
// schema_migrations so re-runs are cheap and idempotent.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect opens a pgx pool and retries until the database is ready (Postgres
// containers accept connections a few seconds after the process starts).
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns < 12 {
		cfg.MaxConns = 12
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second

	deadline := time.Now().Add(90 * time.Second)
	var pool *pgxpool.Pool
	for {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database not ready after retries: %w", err)
		}
		log.Printf("db: waiting for database to become ready (%v)", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Migrate applies every embedded migration file (sorted by name) that has not
// yet been recorded in schema_migrations. Each file's statements are split
// respecting dollar-quotes, single-quoted strings and line comments, then run
// individually in autocommit.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		raw, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		stmts := splitStatements(string(raw))
		log.Printf("db: applying migration %s (%d statements)", name, len(stmts))
		for i, stmt := range stmts {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s statement %d failed: %w\n--- statement ---\n%s", name, i+1, err, stmt)
			}
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// splitStatements splits a SQL script into individual statements on top-level
// semicolons, correctly skipping semicolons that appear inside:
//   - single-quoted string literals ('...'; with '' escape)
//   - dollar-quoted strings ($$...$$ or $tag$...$tag$)
//   - line comments (-- ... end of line)
//   - block comments (/* ... */)
//
// Empty statements (whitespace/comment only) are dropped.
func splitStatements(sql string) []string {
	var stmts []string
	var buf strings.Builder

	runes := []rune(sql)
	n := len(runes)
	i := 0

	flush := func() {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			stmts = append(stmts, s)
		}
		buf.Reset()
	}

	for i < n {
		ch := runes[i]

		// Line comment: -- ... \n
		if ch == '-' && i+1 < n && runes[i+1] == '-' {
			for i < n && runes[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment: /* ... */
		if ch == '/' && i+1 < n && runes[i+1] == '*' {
			i += 2
			for i+1 < n && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		// Single-quoted string literal.
		if ch == '\'' {
			buf.WriteRune(ch)
			i++
			for i < n {
				if runes[i] == '\'' {
					// Escaped quote '' — consume both, stay in string.
					if i+1 < n && runes[i+1] == '\'' {
						buf.WriteRune('\'')
						buf.WriteRune('\'')
						i += 2
						continue
					}
					buf.WriteRune('\'')
					i++
					break
				}
				buf.WriteRune(runes[i])
				i++
			}
			continue
		}
		// Dollar-quoted string: $tag$ ... $tag$ (tag may be empty).
		if ch == '$' {
			if tag, ok := readDollarTag(runes, i); ok {
				buf.WriteString(tag)
				i += len([]rune(tag))
				// Consume until the matching closing tag.
				for i < n {
					if runes[i] == '$' {
						if closeTag, ok := readDollarTag(runes, i); ok && closeTag == tag {
							buf.WriteString(closeTag)
							i += len([]rune(closeTag))
							break
						}
					}
					buf.WriteRune(runes[i])
					i++
				}
				continue
			}
		}
		// Statement terminator.
		if ch == ';' {
			flush()
			i++
			continue
		}
		buf.WriteRune(ch)
		i++
	}
	flush()
	return stmts
}

// readDollarTag returns the dollar-quote tag (e.g. "$$" or "$func$") starting at
// index i, or ok=false if the text at i is not a valid opening/closing tag.
func readDollarTag(runes []rune, i int) (string, bool) {
	n := len(runes)
	if i >= n || runes[i] != '$' {
		return "", false
	}
	j := i + 1
	for j < n {
		c := runes[j]
		if c == '$' {
			return string(runes[i : j+1]), true
		}
		if c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			j++
			continue
		}
		return "", false
	}
	return "", false
}

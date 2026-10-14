package db

import (
	"io/fs"
	"strings"
	"testing"
)

// The runner executes a migration one statement at a time, so a file is only
// as good as the splitter's reading of it. This pins 0015: every statement is
// one the file intends, and none was cut in the middle of a quoted literal.
func TestMigration0015Splits(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/0015_panel_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts := splitStatements(string(raw))
	var alters, comments, creates, inserts int
	for _, s := range stmts {
		switch {
		case strings.HasPrefix(s, "ALTER TABLE admin_user ADD COLUMN IF NOT EXISTS "):
			alters++
		case strings.HasPrefix(s, "COMMENT ON COLUMN admin_user."):
			comments++
		case strings.HasPrefix(s, "CREATE TABLE IF NOT EXISTS app_setting"):
			creates++
		case strings.HasPrefix(s, "INSERT INTO app_setting"):
			inserts++
			if !strings.HasSuffix(s, "ON CONFLICT (key) DO NOTHING") {
				t.Errorf("seed insert is not idempotent: %q", s)
			}
		default:
			t.Errorf("unexpected statement: %q", s)
		}
		if strings.Count(s, "'")%2 != 0 {
			t.Errorf("unbalanced quote (statement cut mid-literal?): %q", s)
		}
	}
	if alters != 9 || comments != 3 || creates != 1 || inserts != 1 {
		t.Errorf("got %d ALTER, %d COMMENT, %d CREATE, %d INSERT", alters, comments, creates, inserts)
	}
	// The reserved-word rule: the logged-on user column is logon_user, and this
	// migration must not introduce a current_user column.
	if strings.Contains(strings.ToLower(string(raw)), "current_user") {
		t.Error("migration mentions current_user")
	}
}

// Migrations are tracked by file name; two files sharing a number would apply
// in an order nobody chose.
func TestMigrationNumbersUnique(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			t.Errorf("unexpected migration name %q", e.Name())
			continue
		}
		if prev, dup := seen[num]; dup {
			t.Errorf("migration number %s used by both %s and %s", num, prev, e.Name())
		}
		seen[num] = e.Name()
	}
}

// 0016 is one DO block: the splitter must keep it whole (its body is full of
// semicolons), and it must be the guarded form that cannot fail on a database
// that already holds case-only duplicate usernames.
func TestMigration0016IsOneGuardedStatement(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/0016_username_ci_unique.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts := splitStatements(string(raw))
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(stmts), stmts)
	}
	s := stmts[0]
	for _, want := range []string{
		"DO $$", "HAVING count(*) > 1", "RAISE WARNING",
		"CREATE UNIQUE INDEX IF NOT EXISTS admin_user_username_lower_idx ON admin_user (lower(username))",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("statement lacks %q", want)
		}
	}
	if strings.Index(s, "IF EXISTS") > strings.Index(s, "CREATE UNIQUE INDEX") {
		t.Error("the index is created before the duplicate check")
	}
}

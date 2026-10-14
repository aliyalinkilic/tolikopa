// Package admincli implements the break-glass subcommands of the server binary:
//
//	server admin init-admin <username>         (password read from stdin)
//	server admin reset-2fa <username>
//	server admin reset-password <username>     (new password read from stdin)
//	server admin require-2fa off
//
// init-admin creates the FIRST administrator and nothing else: it refuses once
// any panel account exists. setup.sh uses it so that a fresh install asks for
// the admin on the server's console instead of exposing a sign-up form.
//
// The others exist for one situation: the last administrator can no longer sign
// in (lost authenticator and recovery codes, forgotten password). They talk to the
// database directly and need only DATABASE_URL, so they work while the HTTP
// server is down or misconfigured. Whoever can run them already has shell access
// to the container and the database credentials — they grant nothing new.
//
// A secret is never accepted on the command line (argv is visible in `ps` and
// lands in shell history); the new password is read from stdin only.
package admincli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/db"
)

const usage = `usage:
  server admin init-admin <username>       create the first administrator (password from stdin)
  server admin reset-2fa <username>        switch two-factor off for a panel user
  server admin reset-password <username>   set a new password (read from stdin)
  server admin require-2fa off             turn the "require 2FA" policy off`

// store is the handful of statements the commands run; an interface so the
// command logic is testable without a database.
type store interface {
	// Reset2FA clears two-factor and ends the user's sessions. false = no such user.
	Reset2FA(ctx context.Context, username string) (bool, error)
	// SetPassword stores a new hash and ends the user's sessions. false = no such user.
	SetPassword(ctx context.Context, username, hash string) (bool, error)
	// Require2FAOff turns the policy off.
	Require2FAOff(ctx context.Context) error
	// CreateFirstAdmin inserts an admin only while no panel account exists.
	// false = an account already exists.
	CreateFirstAdmin(ctx context.Context, username, hash string) (bool, error)
}

// Main runs `server admin <args…>` against the database named by DATABASE_URL
// and returns the process exit code.
func Main(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if err := validate(args); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "error: DATABASE_URL is not set")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot connect to the database")
		return 1
	}
	defer pool.Close()
	return run(ctx, pgStore{pool}, args, os.Stdin, os.Stdout, os.Stderr, isTerminal(os.Stdin))
}

// validate checks the command shape before any connection is made.
func validate(args []string) error {
	switch args[0] {
	case "init-admin":
		if len(args) != 2 || !auth.ValidUsername(args[1]) {
			return errors.New("init-admin takes exactly one argument: a username of letters, digits or . _ - @")
		}
	case "reset-2fa", "reset-password":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("%s takes exactly one argument: the username", args[0])
		}
	case "require-2fa":
		if len(args) != 2 || args[1] != "off" {
			return errors.New(`require-2fa only accepts "off" (turn it on from the panel)`)
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

// run executes one validated command and prints a single result line.
func run(ctx context.Context, st store, args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool) int {
	fail := func(msg string) int {
		fmt.Fprintln(stderr, "error: "+msg)
		return 1
	}
	if err := validate(args); err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 2
	}
	switch args[0] {
	case "init-admin":
		pw, err := readSecret(stdin)
		if err != nil {
			return fail("could not read the password from stdin")
		}
		if err := auth.ValidatePassword(pw); err != nil {
			_, body := apierr.Resolve(err)
			return fail(body.Message)
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return fail("could not hash the password")
		}
		created, err := st.CreateFirstAdmin(ctx, args[1], hash)
		if err != nil {
			return fail("database error")
		}
		if !created {
			return fail("a panel account already exists; init-admin only creates the first one")
		}
		fmt.Fprintf(stdout, "ok: administrator %q created\n", args[1])

	case "reset-2fa":
		ok, err := st.Reset2FA(ctx, args[1])
		if err != nil {
			return fail("database error")
		}
		if !ok {
			return fail(fmt.Sprintf("no panel user named %q", args[1]))
		}
		fmt.Fprintf(stdout, "ok: two-factor reset for %q; all sessions signed out\n", args[1])

	case "reset-password":
		if interactive {
			// No terminal handling without a dependency: the input is echoed.
			fmt.Fprint(stderr, "New password (input is visible): ")
		}
		pw, err := readSecret(stdin)
		if err != nil {
			return fail("could not read the new password from stdin")
		}
		if err := auth.ValidatePassword(pw); err != nil {
			_, body := apierr.Resolve(err)
			return fail(body.Message)
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return fail("could not hash the password")
		}
		ok, err := st.SetPassword(ctx, args[1], hash)
		if err != nil {
			return fail("database error")
		}
		if !ok {
			return fail(fmt.Sprintf("no panel user named %q", args[1]))
		}
		fmt.Fprintf(stdout, "ok: password reset for %q; all sessions signed out\n", args[1])

	case "require-2fa":
		if err := st.Require2FAOff(ctx); err != nil {
			return fail("database error")
		}
		fmt.Fprintln(stdout, "ok: require_2fa is off (a running server applies it within 15 seconds)")
	}
	return 0
}

// readSecret reads one line from r and strips the line terminator only —
// leading/trailing spaces are part of the password.
func readSecret(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 4096)).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ── database implementation ──────────────────────────────────────────────────

type pgStore struct{ pool *pgxpool.Pool }

func (s pgStore) Reset2FA(ctx context.Context, username string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		auth.ClearTOTPSQL+`, session_epoch=session_epoch+1 WHERE username=$1`, username)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s pgStore) SetPassword(ctx context.Context, username, hash string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE admin_user SET password_hash=$2, session_epoch=session_epoch+1 WHERE username=$1`,
		username, hash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s pgStore) CreateFirstAdmin(ctx context.Context, username, hash string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	// Same lock as first-run setup in the panel: "the table is empty" and the
	// insert are one step, whichever path gets there first.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auth.PanelUserLockKey); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO admin_user (username, password_hash, role)
		SELECT $1, $2, 'admin'
		WHERE NOT EXISTS (SELECT 1 FROM admin_user)`, username, hash)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s pgStore) Require2FAOff(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, 'false'::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		auth.SettingRequire2FA)
	return err
}

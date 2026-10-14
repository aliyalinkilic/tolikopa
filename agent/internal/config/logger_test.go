package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingWriter stands in for os.Stderr inside a Windows service: an invalid
// handle whose every write returns an error.
type failingWriter struct{ writes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, errors.New("handle is invalid")
}

// TestFanoutWriterSurvivesAFailingSink is the regression guard for the bug that
// silenced agent.log fleet-wide: io.MultiWriter stops at the first sink that
// errors, so a broken stderr meant the file was never written. If this ever
// reverts, the agent goes blind again and nobody notices until they need a log.
func TestFanoutWriterSurvivesAFailingSink(t *testing.T) {
	bad := &failingWriter{}
	var good bytes.Buffer

	// Order matters: the failing sink is FIRST, exactly as stderr was.
	w := fanoutWriter{[]io.Writer{bad, &good}}
	n, err := w.Write([]byte("hello\n"))

	if err != nil {
		t.Errorf("Write returned %v; a logger must never fail the caller", err)
	}
	if n != 6 {
		t.Errorf("Write returned n=%d, want 6", n)
	}
	if bad.writes != 1 {
		t.Errorf("failing sink was written %d times, want 1", bad.writes)
	}
	if got := good.String(); got != "hello\n" {
		t.Errorf("second sink got %q, want %q — a failing sink swallowed the write", got, "hello\n")
	}
}

func TestFanoutWriterWithNoUsableSink(t *testing.T) {
	// Both sinks broken: still must not return an error to the caller.
	w := fanoutWriter{[]io.Writer{&failingWriter{}, &failingWriter{}}}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Errorf("Write returned %v, want nil", err)
	}
}

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")

	// Under the cap: left alone.
	if err := os.WriteFile(path, []byte("small"), 0o640); err != nil {
		t.Fatal(err)
	}
	rotate(path)
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("rotated a file that was under the cap")
	}

	// Over the cap: moved aside, exactly one generation kept.
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLogBytes+1), 0o640); err != nil {
		t.Fatal(err)
	}
	rotate(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("current log still present after rotation")
	}
	if fi, err := os.Stat(path + ".1"); err != nil || fi.Size() != int64(maxLogBytes+1) {
		t.Errorf("previous generation missing or wrong size: %v", err)
	}
}

func TestNewLoggerWritesToFile(t *testing.T) {
	// LogDir() is derived from ProgramData/base dir; point it at a temp tree.
	t.Setenv("ProgramData", t.TempDir())

	log := NewLogger("info")
	log.Info("hygiene pass finished", "items", 3)

	b, err := os.ReadFile(filepath.Join(LogDir(), "agent.log"))
	if err != nil {
		t.Fatalf("agent.log was not written: %v", err)
	}
	if !strings.Contains(string(b), "hygiene pass finished") {
		t.Errorf("agent.log = %q, want it to contain the logged message", string(b))
	}
}

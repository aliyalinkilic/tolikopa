package main

import (
	"os"
	"strings"
	"testing"
)

// The repository's VERSION file is the single source of version numbers; the
// image build stamps it into the agent with -ldflags. The default here only
// serves local builds, and must not drift from it.
func TestDefaultVersionMatchesVersionFile(t *testing.T) {
	raw, err := os.ReadFile("../../../VERSION")
	if err != nil {
		t.Skip("VERSION not found next to the agent module:", err)
	}
	if want := strings.TrimSpace(string(raw)); version != want {
		t.Fatalf("agent default version %q differs from VERSION %q; update cmd/agent/main.go", version, want)
	}
}

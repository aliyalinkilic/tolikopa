package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Local development has no artifacts directory; nothing may panic or succeed.
func TestMissingDirectoryDegrades(t *testing.T) {
	for _, st := range []*Store{New(""), New(filepath.Join(t.TempDir(), "does-not-exist"))} {
		if _, err := st.Info(); !errors.Is(err, ErrUnavailable) {
			t.Errorf("Info: %v, want ErrUnavailable", err)
		}
		if _, _, err := st.Read(AgentWindows); !errors.Is(err, ErrUnavailable) {
			t.Errorf("Read: %v, want ErrUnavailable", err)
		}
		if _, err := st.Sum(SetupTool); !errors.Is(err, ErrUnavailable) {
			t.Errorf("Sum: %v, want ErrUnavailable", err)
		}
	}
}

func TestInfoNeedsEveryRolloutFile(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	write(t, dir, metaFile, `{"product_version":"2.0.0","agent_version":" 1.9.0 ","built_at":"2026-10-13T08:00:00Z"}`)
	write(t, dir, AgentWindows, "agent")
	write(t, dir, Bootstrap, "ps1")
	if _, err := st.Info(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Info without the setup tool: %v", err)
	}
	write(t, dir, SetupTool, "tool")
	info, err := st.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.ProductVersion != "2.0.0" || info.AgentVersion != "1.9.0" || info.BuiltAt != "2026-10-13T08:00:00Z" {
		t.Errorf("info = %+v", info)
	}
	if info.AgentSHA256 != SumBytes([]byte("agent")) {
		t.Errorf("agent_sha256 = %s", info.AgentSHA256)
	}

	write(t, dir, metaFile, `{not json`)
	if _, err := st.Info(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("broken artifacts.json: %v", err)
	}
	write(t, dir, metaFile, `{"product_version":"2.0.0"}`)
	if _, err := st.Info(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("artifacts.json without agent_version: %v", err)
	}
}

func TestSumIsCachedUntilTheFileChanges(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	write(t, dir, AgentWindows, "version-one")
	first, err := st.Sum(AgentWindows)
	if err != nil || first != SumBytes([]byte("version-one")) {
		t.Fatalf("Sum = %s, %v", first, err)
	}
	if _, cached := st.sums[AgentWindows]; !cached {
		t.Fatal("hash was not cached")
	}

	// Same size, new content, new mtime: must be noticed.
	write(t, dir, AgentWindows, "version-two")
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, AgentWindows), later, later); err != nil {
		t.Fatal(err)
	}
	second, err := st.Sum(AgentWindows)
	if err != nil || second != SumBytes([]byte("version-two")) {
		t.Errorf("stale hash after the file changed: %s, %v", second, err)
	}

	data, sum, err := st.Read(AgentWindows)
	if err != nil || string(data) != "version-two" || sum != second {
		t.Errorf("Read = %q %s %v", data, sum, err)
	}
}

// Names are a closed set: nothing outside it is ever opened.
func TestOnlyKnownNames(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	write(t, dir, "secret.txt", "x")
	write(t, dir, metaFile, `{"agent_version":"1"}`)
	for _, name := range []string{"secret.txt", "../secret.txt", metaFile, "", `..\..\etc\passwd`, "/etc/passwd"} {
		if _, _, err := st.Read(name); !errors.Is(err, ErrUnavailable) {
			t.Errorf("Read(%q) = %v, want ErrUnavailable", name, err)
		}
	}
	if _, err := st.Sum("secret.txt"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Sum of an unknown name: %v", err)
	}
}

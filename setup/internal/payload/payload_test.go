package payload

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		Mode: ModeSetup, Lang: "en", DomainFQDN: "corp.example.com", NetBIOS: "CORP",
		TargetDN: "OU=Workstations,DC=corp,DC=example,DC=com", GPOName: "Tolikopa Agent",
		Files: []File{
			{Name: "tolikopa-agent.exe", SHA256: Sum([]byte("a"))},
			{Name: "bootstrap.ps1", SHA256: Sum([]byte("b"))},
			{Name: "config.json", SHA256: Sum([]byte("c"))},
		},
	}
}

func TestValidate(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := map[string]func(*Manifest){
		"mode":             func(m *Manifest) { m.Mode = "exec" },
		"fqdn injection":   func(m *Manifest) { m.DomainFQDN = "corp.example.com; calc" },
		"fqdn single":      func(m *Manifest) { m.DomainFQDN = "corp" },
		"fqdn ip":          func(m *Manifest) { m.DomainFQDN = "10.0.0.99"; m.TargetDN = "" },
		"fqdn numeric tld": func(m *Manifest) { m.DomainFQDN = "corp.123"; m.TargetDN = "" },
		"gpo quote":        func(m *Manifest) { m.GPOName = `x" -Evil` },
		"gpo subexpr":      func(m *Manifest) { m.GPOName = "$(calc)" },
		"dn backtick":      func(m *Manifest) { m.TargetDN = "OU=a`b,DC=corp,DC=example,DC=com" },
		"dn other domain":  func(m *Manifest) { m.TargetDN = "OU=PCs,DC=other,DC=example,DC=com" },
		"file traversal":   func(m *Manifest) { m.Files[0].Name = `..\tolikopa-agent.exe` },
		"file unexpected":  func(m *Manifest) { m.Files[0].Name = "payload.dll" },
		"file duplicate":   func(m *Manifest) { m.Files[1].Name = "tolikopa-agent.exe" },
		"sha":              func(m *Manifest) { m.Files[0].SHA256 = "zz" },
		"setup incomplete": func(m *Manifest) { m.Files = m.Files[:2] },
		"version escape":   func(m *Manifest) { m.AgentVersion = "2.0.0\x1b[2J" },
		"url scheme":       func(m *Manifest) { m.ServerURL = "file://x" },
		"url plain http":   func(m *Manifest) { m.ServerURL = "http://tolikopa.corp.example.com" },
		"url userinfo":     func(m *Manifest) { m.ServerURL = "https://a@evil.example" },
		"url control":      func(m *Manifest) { m.ServerURL = "https://x\r\nOK" },
		"url unicode":      func(m *Manifest) { m.ServerURL = "https://tolikopa.еxample" },
	}
	for name, mutate := range bad {
		m := validManifest()
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}

	upd := validManifest()
	upd.Mode, upd.GPOName, upd.TargetDN = ModeUpdate, "", ""
	upd.Files = upd.Files[:1]
	if err := upd.Validate(); err != nil {
		t.Errorf("update bundle with tolikopa-agent.exe only rejected: %v", err)
	}
}

func TestPaths(t *testing.T) {
	m := validManifest()
	if got := m.SharePath(); got != `\\corp.example.com\NETLOGON\Tolikopa` {
		t.Errorf("SharePath = %s", got)
	}
	m.TargetDN = ""
	if got := m.LinkTarget(); got != "DC=corp,DC=example,DC=com" {
		t.Errorf("LinkTarget = %s", got)
	}
}

// bundleZip returns a bundle archive for m with the three standard files.
func bundleZip(t *testing.T, m Manifest) []byte {
	t.Helper()
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	mj, _ := json.Marshal(m)
	for name, body := range map[string][]byte{
		"manifest.json": mj, "tolikopa-agent.exe": []byte("a"), "bootstrap.ps1": []byte("b"), "config.json": []byte("c"),
	} {
		w, _ := zw.Create(name)
		w.Write(body)
	}
	zw.Close()
	return zbuf.Bytes()
}

var fakeExe = []byte("MZ" + strings.Repeat("\x00binary", 5000))

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Lookup order: appended to the executable first, then tolikopa.bundle in the
// executable's directory, and nowhere else.
func TestLocate(t *testing.T) {
	appended := validManifest()
	appended.GPOName = "Appended"
	sidecar := validManifest()
	sidecar.GPOName = "Sidecar"

	t.Run("appended wins over the sidecar", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "tolikopa-setup.exe")
		writeFile(t, exe, append(append([]byte{}, fakeExe...), bundleZip(t, appended)...))
		writeFile(t, filepath.Join(dir, BundleFileName), bundleZip(t, sidecar))
		b, from, err := Locate(exe)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		if from != exe || b.Manifest.GPOName != "Appended" {
			t.Errorf("read %s (%s), want the appended bundle", from, b.Manifest.GPOName)
		}
	})

	t.Run("sidecar next to a plain executable", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "tolikopa-update.exe")
		writeFile(t, exe, fakeExe)
		writeFile(t, filepath.Join(dir, BundleFileName), bundleZip(t, sidecar))
		b, from, err := Locate(exe)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		if from != filepath.Join(dir, BundleFileName) || b.Manifest.GPOName != "Sidecar" {
			t.Errorf("read %s (%s), want the sidecar", from, b.Manifest.GPOName)
		}
		if data, err := b.Read(b.Manifest.Files[0]); err != nil || string(data) != "a" {
			t.Errorf("Read = %q, %v", data, err)
		}
	})

	t.Run("working directory is never searched", func(t *testing.T) {
		exeDir, cwd := t.TempDir(), t.TempDir()
		exe := filepath.Join(exeDir, "tolikopa-setup.exe")
		writeFile(t, exe, fakeExe)
		writeFile(t, filepath.Join(cwd, BundleFileName), bundleZip(t, sidecar))
		t.Chdir(cwd)
		b, _, err := Locate(exe)
		if !errors.Is(err, ErrNotFound) {
			if b != nil {
				b.Close()
			}
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid appended bundle does not fall through", func(t *testing.T) {
		dir := t.TempDir()
		bad := validManifest()
		bad.GPOName = "$(calc)"
		exe := filepath.Join(dir, "tolikopa-setup.exe")
		writeFile(t, exe, append(append([]byte{}, fakeExe...), bundleZip(t, bad)...))
		writeFile(t, filepath.Join(dir, BundleFileName), bundleZip(t, sidecar))
		if b, _, err := Locate(exe); err == nil || errors.Is(err, ErrNotFound) {
			if b != nil {
				b.Close()
			}
			t.Fatalf("got %v, want a validation error", err)
		}
	})

	t.Run("invalid sidecar is rejected", func(t *testing.T) {
		dir := t.TempDir()
		bad := validManifest()
		bad.DomainFQDN = "corp.example.com; calc"
		exe := filepath.Join(dir, "tolikopa-setup.exe")
		writeFile(t, exe, fakeExe)
		writeFile(t, filepath.Join(dir, BundleFileName), bundleZip(t, bad))
		if b, _, err := Locate(exe); err == nil || errors.Is(err, ErrNotFound) {
			if b != nil {
				b.Close()
			}
			t.Fatalf("got %v, want a validation error", err)
		}
	})

	t.Run("sidecar that is a directory", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "tolikopa-setup.exe")
		writeFile(t, exe, fakeExe)
		if err := os.Mkdir(filepath.Join(dir, BundleFileName), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Locate(exe); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v, want an error", err)
		}
	})
}

// Older downloads appended the zip to the executable; the bundle must open from
// a file that has arbitrary bytes in front of the archive.
func TestOpenWithPrefix(t *testing.T) {
	m := validManifest()
	path := filepath.Join(t.TempDir(), "tolikopa-setup.exe")
	if err := os.WriteFile(path, append(append([]byte{}, fakeExe...), bundleZip(t, m)...), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()
	data, err := b.Read(b.Manifest.Files[0])
	if err != nil || string(data) != "a" {
		t.Fatalf("Read = %q, %v", data, err)
	}
	if _, err := b.Read(File{Name: "tolikopa-agent.exe", SHA256: Sum([]byte("tampered"))}); err == nil {
		t.Error("checksum mismatch not detected")
	}
}

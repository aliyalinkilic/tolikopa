package payload

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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

// The server appends the zip to the executable; the bundle must open from a file
// that has arbitrary bytes in front of the archive.
func TestOpenWithPrefix(t *testing.T) {
	m := validManifest()
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

	path := filepath.Join(t.TempDir(), "tolikopa-setup.exe")
	prefix := []byte("MZ" + strings.Repeat("\x00binary", 5000))
	if err := os.WriteFile(path, append(prefix, zbuf.Bytes()...), 0o600); err != nil {
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

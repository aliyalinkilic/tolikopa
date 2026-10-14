package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/artifacts"
)

const testToken = "INGEST-TOKEN-ONLY-IN-CONFIG-JSON"

// fakeArtifacts writes an artifacts directory the way the image provides it.
func fakeArtifacts(t *testing.T) *artifacts.Store {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		artifacts.SetupTool:    "MZ" + strings.Repeat("\x00setup-tool-binary", 4000),
		artifacts.AgentWindows: "MZ" + strings.Repeat("agent-windows", 3000),
		artifacts.AgentLinux:   "\x7fELF" + strings.Repeat("agent-linux", 3000),
		artifacts.Bootstrap:    "# bootstrap.ps1\r\nWrite-Host 'hi'\r\n",
		"artifacts.json":       `{"product_version":"2.0.0","agent_version":"1.9.0","built_at":"2026-10-13T08:00:00Z"}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return artifacts.New(dir)
}

func validManifest() Manifest {
	return Manifest{
		Mode: ModeSetup, Lang: "en", DomainFQDN: "corp.example.com", NetBIOS: "CORP",
		TargetDN: "OU=Workstations,DC=corp,DC=example,DC=com", GPOName: "Tolikopa Agent",
		Files: []File{
			{Name: FileAgent, SHA256: artifacts.SumBytes([]byte("a"))},
			{Name: FileBootstrap, SHA256: artifacts.SumBytes([]byte("b"))},
			{Name: FileConfig, SHA256: artifacts.SumBytes([]byte("c"))},
		},
	}
}

// strictManifest decodes exactly as the tool does: unknown fields are an error.
func strictManifest(t *testing.T, raw []byte) Manifest {
	t.Helper()
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("manifest does not decode strictly: %v", err)
	}
	return m
}

// End to end: fake artifacts → assembled download → written to disk with the
// exe in front → re-opened with archive/zip exactly as the tool opens itself.
func TestAssembleRoundTrip(t *testing.T) {
	st := fakeArtifacts(t)
	meta, err := st.Meta()
	if err != nil {
		t.Fatal(err)
	}
	exe, _, _ := st.Read(artifacts.SetupTool)
	agent, agentSum, _ := st.Read(artifacts.AgentWindows)
	boot, _, _ := st.Read(artifacts.Bootstrap)
	conf, err := NewAgentConfig("https://tolikopa.corp.example.com", testToken, "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n").JSON()
	if err != nil {
		t.Fatal(err)
	}
	contents := []Content{{FileAgent, agent}, {FileBootstrap, boot}, {FileConfig, conf}}

	for _, mode := range []string{ModeSetup, ModeUpdate} {
		m := Manifest{
			Mode: mode, Lang: "tr", ProductVersion: meta.ProductVersion, AgentVersion: meta.AgentVersion,
			ServerURL: "https://tolikopa.corp.example.com", DomainFQDN: "corp.example.com", NetBIOS: "CORP",
		}
		if mode == ModeSetup {
			m.TargetDN, m.GPOName, m.DefenderExclusion = "OU=Workstations,DC=corp,DC=example,DC=com", "Tolikopa Agent", true
		}
		out, err := Assemble(exe, m, contents)
		if err != nil {
			t.Fatalf("%s: Assemble: %v", mode, err)
		}
		if !bytes.HasPrefix(out, exe) {
			t.Fatalf("%s: the executable is not the unmodified prefix of the download", mode)
		}

		path := filepath.Join(t.TempDir(), "tolikopa-"+mode+".exe")
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("%s: archive/zip cannot open the download: %v", mode, err)
		}
		got := map[string][]byte{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("%s: open %s: %v", mode, f.Name, err)
			}
			got[f.Name], _ = io.ReadAll(rc)
			rc.Close()
		}
		zr.Close()

		if len(got) != 4 {
			t.Errorf("%s: archive has %d entries, want manifest + 3 files", mode, len(got))
		}
		mf := strictManifest(t, got["manifest.json"])
		if err := mf.Validate(); err != nil {
			t.Errorf("%s: emitted manifest fails validation: %v", mode, err)
		}
		if mf.Mode != mode || mf.Lang != "tr" || mf.ProductVersion != "2.0.0" || mf.AgentVersion != "1.9.0" ||
			mf.ServerURL != "https://tolikopa.corp.example.com" || mf.DomainFQDN != "corp.example.com" || mf.NetBIOS != "CORP" {
			t.Errorf("%s: manifest fields: %+v", mode, mf)
		}
		if mode == ModeSetup && (mf.GPOName != "Tolikopa Agent" || mf.TargetDN == "" || !mf.DefenderExclusion) {
			t.Errorf("setup manifest lost its GPO fields: %+v", mf)
		}
		if mode == ModeUpdate && (mf.GPOName != "" || mf.TargetDN != "" || mf.DefenderExclusion) {
			t.Errorf("update manifest carries GPO fields: %+v", mf)
		}
		if len(mf.Files) != 3 {
			t.Fatalf("%s: manifest lists %d files", mode, len(mf.Files))
		}
		for _, f := range mf.Files {
			body, ok := got[f.Name]
			if !ok {
				t.Errorf("%s: %s is in the manifest but not in the archive", mode, f.Name)
				continue
			}
			if sum := artifacts.SumBytes(body); sum != f.SHA256 {
				t.Errorf("%s: %s hash mismatch: manifest %s, content %s", mode, f.Name, f.SHA256, sum)
			}
		}
		if !bytes.Equal(got[FileAgent], agent) || mf.Files[0].SHA256 != agentSum {
			t.Errorf("%s: bundled agent differs from the artifact", mode)
		}
		// The ingest token travels in config.json and nowhere else.
		if bytes.Contains(got["manifest.json"], []byte(testToken)) {
			t.Errorf("%s: the ingest token is in the manifest", mode)
		}
		if !bytes.Contains(got[FileConfig], []byte(testToken)) {
			t.Errorf("%s: config.json lacks the ingest token", mode)
		}
	}
}

func TestAssembleRefusesInvalidManifest(t *testing.T) {
	m := validManifest()
	m.DomainFQDN = "corp.example.com; calc"
	_, err := Assemble([]byte("MZ"), m, []Content{{FileAgent, []byte("a")}, {FileBootstrap, []byte("b")}, {FileConfig, []byte("c")}})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "domain_fqdn" {
		t.Errorf("got %v, want a domain_fqdn FieldError", err)
	}
	if _, err := Assemble(nil, validManifest(), nil); err == nil {
		t.Error("empty executable accepted")
	}
	// A file outside the tool's allowlist can never be bundled.
	if _, err := Assemble([]byte("MZ"), validManifest(), []Content{{"payload.dll", []byte("x")}}); err == nil {
		t.Error("unexpected file name accepted")
	}
}

// The same cases as setup/internal/payload TestValidate, plus more injection
// attempts: whatever the tool rejects, the server must reject first.
func TestValidateParity(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := map[string]func(*Manifest){
		"mode":             func(m *Manifest) { m.Mode = "exec" },
		"fqdn injection":   func(m *Manifest) { m.DomainFQDN = "corp.example.com; calc" },
		"fqdn single":      func(m *Manifest) { m.DomainFQDN = "corp" },
		"fqdn empty":       func(m *Manifest) { m.DomainFQDN = "" },
		"fqdn unc":         func(m *Manifest) { m.DomainFQDN = `evil.example.com\share` },
		"fqdn slash":       func(m *Manifest) { m.DomainFQDN = "corp.example.com/x" },
		"fqdn space":       func(m *Manifest) { m.DomainFQDN = "corp.example.com -x" },
		"fqdn newline":     func(m *Manifest) { m.DomainFQDN = "corp.example.com\nx.y" },
		"fqdn underscore":  func(m *Manifest) { m.DomainFQDN = "co_rp.example.com" },
		"fqdn too long":    func(m *Manifest) { m.DomainFQDN = strings.Repeat("a.", 130) + "com" },
		"fqdn ipv4":        func(m *Manifest) { m.DomainFQDN = "10.0.0.99" },
		"fqdn numeric tld": func(m *Manifest) { m.DomainFQDN = "corp.123" },
		"fqdn short ip":    func(m *Manifest) { m.DomainFQDN = "10.1" },
		"fqdn host.ip":     func(m *Manifest) { m.DomainFQDN = "evil.10.0.0.99" },
		"netbios dollar":   func(m *Manifest) { m.NetBIOS = "CORP$" },
		"netbios long":     func(m *Manifest) { m.NetBIOS = "ABCDEFGHIJKLMNOP" },
		"gpo quote":        func(m *Manifest) { m.GPOName = `x" -Evil` },
		"gpo subexpr":      func(m *Manifest) { m.GPOName = "$(calc)" },
		"gpo semicolon":    func(m *Manifest) { m.GPOName = "Agent; calc" },
		"gpo single quote": func(m *Manifest) { m.GPOName = "Agent' x" },
		"gpo backtick":     func(m *Manifest) { m.GPOName = "Agent`n" },
		"gpo leading dash": func(m *Manifest) { m.GPOName = "-Name" },
		"gpo empty":        func(m *Manifest) { m.GPOName = "" },
		"gpo too long":     func(m *Manifest) { m.GPOName = strings.Repeat("a", 64) },
		"dn backtick":      func(m *Manifest) { m.TargetDN = "OU=a`b,DC=corp,DC=example,DC=com" },
		"dn other domain":  func(m *Manifest) { m.TargetDN = "OU=PCs,DC=other,DC=example,DC=com" },
		"dn subexpr":       func(m *Manifest) { m.TargetDN = "OU=$(calc),DC=corp,DC=example,DC=com" },
		"dn semicolon":     func(m *Manifest) { m.TargetDN = "OU=a;b,DC=corp,DC=example,DC=com" },
		"dn quote":         func(m *Manifest) { m.TargetDN = `OU="a",DC=corp,DC=example,DC=com` },
		"dn escape":        func(m *Manifest) { m.TargetDN = `OU=a\,b,DC=corp,DC=example,DC=com` },
		"dn bad attr":      func(m *Manifest) { m.TargetDN = "UID=x,DC=corp,DC=example,DC=com" },
		"dn too long":      func(m *Manifest) { m.TargetDN = "OU=" + strings.Repeat("a", 520) + ",DC=corp,DC=example,DC=com" },
		"file traversal":   func(m *Manifest) { m.Files[0].Name = `..\tolikopa-agent.exe` },
		"file unexpected":  func(m *Manifest) { m.Files[0].Name = "payload.dll" },
		"file duplicate":   func(m *Manifest) { m.Files[1].Name = "tolikopa-agent.exe" },
		"sha":              func(m *Manifest) { m.Files[0].SHA256 = "zz" },
		"sha uppercase":    func(m *Manifest) { m.Files[0].SHA256 = strings.ToUpper(m.Files[0].SHA256) },
		"setup incomplete": func(m *Manifest) { m.Files = m.Files[:2] },
		"no files":         func(m *Manifest) { m.Files = nil },
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
	root := validManifest()
	root.TargetDN, root.NetBIOS = "", ""
	if err := root.Validate(); err != nil {
		t.Errorf("empty target_dn (domain root) / empty netbios rejected: %v", err)
	}
	// Digits are fine anywhere but as the whole last label.
	for _, ok := range []string{"corp.example.com", "123.example.com", "corp2.example4.com", "corp.x1", "corp.1x"} {
		if !ValidFQDN(ok) {
			t.Errorf("ValidFQDN(%q) = false", ok)
		}
	}
	for in, want := range map[string]bool{"a.b": false, "a.1": true, "1.2.3.4": true, "a.b1": false, "x": false, "007": true, "": false} {
		if numericTLD(in) != want {
			t.Errorf("numericTLD(%q) = %v, want %v", in, !want, want)
		}
	}
	if got := DomainDN("corp.example.com"); got != "DC=corp,DC=example,DC=com" {
		t.Errorf("DomainDN = %s", got)
	}
}

// manifestFields is the wire contract with the setup tool, which decodes with
// DisallowUnknownFields. Changing this list means changing payload.Manifest in
// the setup module in the same commit.
var manifestFields = []string{
	"mode", "lang", "product_version", "agent_version", "server_url", "domain_fqdn",
	"netbios", "target_dn", "gpo_name", "defender_exclusion", "files",
}

func jsonTags(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Tag.Get("json"))
	}
	return out
}

func TestManifestJSONFields(t *testing.T) {
	if got := jsonTags(reflect.TypeOf(Manifest{})); !reflect.DeepEqual(got, manifestFields) {
		t.Errorf("Manifest JSON fields\n got %v\nwant %v", got, manifestFields)
	}
	if got := jsonTags(reflect.TypeOf(File{})); !reflect.DeepEqual(got, []string{"name", "sha256"}) {
		t.Errorf("File JSON fields = %v", got)
	}
	// Every field is always emitted (no omitempty), with exactly these keys.
	raw, _ := json.Marshal(Manifest{Files: []File{}})
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	if len(keys) != len(manifestFields) {
		t.Errorf("marshalled manifest has %d keys, want %d: %s", len(keys), len(manifestFields), raw)
	}
	for _, k := range manifestFields {
		if _, ok := keys[k]; !ok {
			t.Errorf("marshalled manifest lacks %q", k)
		}
	}
}

// When the setup module is checked out next to this one (the normal repository
// layout; not the case inside the server's Docker build context), compare the
// mirrored regexes and field names against the real source. The two modules
// must not import each other, so this is how drift is caught.
func TestMirrorMatchesSetupModule(t *testing.T) {
	theirs, err := os.ReadFile(filepath.Join("..", "..", "..", "setup", "internal", "payload", "payload.go"))
	if err != nil {
		t.Skip("setup module source not available: " + err.Error())
	}
	ours, err := os.ReadFile("bundle.go")
	if err != nil {
		t.Fatal(err)
	}
	reDecl := regexp.MustCompile("(?m)^\\s*(re[A-Za-z]+)\\s*=\\s*regexp\\.MustCompile\\(`([^`]+)`\\)")
	collect := func(src []byte) map[string]string {
		out := map[string]string{}
		for _, m := range reDecl.FindAllSubmatch(src, -1) {
			out[string(m[1])] = string(m[2])
		}
		return out
	}
	mine, tool := collect(ours), collect(theirs)
	if len(tool) == 0 {
		t.Fatal("found no regexes in the setup module source; the test needs updating")
	}
	for name, expr := range tool {
		if mine[name] != expr {
			t.Errorf("%s differs from the setup tool\n tool: %s\n ours: %s", name, expr, mine[name])
		}
	}

	// Field names of the tool's Manifest, in order.
	start := bytes.Index(theirs, []byte("type Manifest struct {"))
	if start < 0 {
		t.Fatal("payload.Manifest not found in the setup module source")
	}
	body := theirs[start:]
	body = body[:bytes.Index(body, []byte("\n}"))]
	var toolFields []string
	for _, m := range regexp.MustCompile("`json:\"([a-z0-9_]+)\"`").FindAllSubmatch(body, -1) {
		toolFields = append(toolFields, string(m[1]))
	}
	if !reflect.DeepEqual(toolFields, manifestFields) {
		t.Errorf("payload.Manifest fields changed\n tool: %v\n ours: %v", toolFields, manifestFields)
	}
}

// CLAUDE.md "Safety", invariant 4: the server can never set the local kill
// switches. The generated config.json must not even mention them.
func TestAgentConfigNeverCarriesKillSwitches(t *testing.T) {
	forbidden := []string{"allow_app_removal", "allow_startup_control", "enforce_actions", "hygiene_hours"}

	raw, err := NewAgentConfig("https://10.0.0.10", testToken, "PEM").JSON()
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range forbidden {
		if _, ok := keys[k]; ok {
			t.Errorf("config.json contains the kill switch %q", k)
		}
		if bytes.Contains(raw, []byte(k)) {
			t.Errorf("config.json mentions %q", k)
		}
	}
	// And the type itself has no such field, so nobody can populate one later
	// without this test failing.
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			for _, k := range forbidden {
				if tag == k || strings.Contains(strings.ToLower(f.Name), strings.ReplaceAll(k, "_", "")) {
					t.Errorf("AgentConfig has a kill-switch field: %s (%s)", f.Name, tag)
				}
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type)
			}
		}
	}
	walk(reflect.TypeOf(AgentConfig{}))

	want := []string{"backend_url", "ingest_token", "tls_ca_pem", "intervals", "top_n", "spool_max_mb", "log_level"}
	if len(keys) != len(want) {
		t.Errorf("config.json has %d keys, want exactly %v", len(keys), want)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("config.json lacks %q", k)
		}
	}
	var c AgentConfig
	_ = json.Unmarshal(raw, &c)
	if c.BackendURL != "https://10.0.0.10" || c.IngestToken != testToken || c.TLSCAPem != "PEM" ||
		c.Intervals != (AgentIntervals{15, 60, 60, 45}) || c.TopN != 10 || c.SpoolMaxMB != 50 || c.LogLevel != "info" {
		t.Errorf("defaults: %+v", c)
	}
}

func TestNormalizeServerURL(t *testing.T) {
	ok := map[string]string{
		"https://tolikopa.corp.example.com":      "https://tolikopa.corp.example.com",
		"https://Tolikopa.Corp.Example.com/":     "https://tolikopa.corp.example.com",
		" https://10.0.0.10 ":                    "https://10.0.0.10",
		"https://10.0.0.10:8443":                 "https://10.0.0.10:8443",
		"https://tolikopa.corp.example.com:443/": "https://tolikopa.corp.example.com",
		"https://tolikopa":                       "https://tolikopa",
	}
	for in, want := range ok {
		if got, err := NormalizeServerURL(in); err != nil || got != want {
			t.Errorf("NormalizeServerURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "http://10.0.0.10", "10.0.0.10", "ftp://x.example.com", "https://",
		"https://user:pw@x.example.com", "https://x.example.com/api", "https://x.example.com?a=1",
		"https://x.example.com#f", "https://x example.com", "https://x.example.com:99999999",
		"https://x_y.example.com", `https://x.example.com"`, "https://" + strings.Repeat("a", 300) + ".com",
	} {
		if got, err := NormalizeServerURL(bad); err == nil {
			t.Errorf("NormalizeServerURL(%q) accepted as %q", bad, got)
		}
	}
}

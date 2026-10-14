package api

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/artifacts"
	"github.com/aliyalinkilic/tolikopa/server/internal/bundle"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

const apiTestToken = "tok_SECRET_ingest_value_0123456789"

func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// testAPI builds an API with a fake artifacts dir and root CA, and no database
// (the functions under test do not touch it).
func testAPI(t *testing.T, withArtifacts, withCA bool) *API {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{IngestToken: apiTestToken, ServerHost: "tolikopa.corp.example.com 10.0.0.10"}
	if withCA {
		cfg.TLSCertFile = filepath.Join(dir, "root.crt")
		// A private key in the same file must never be passed on to agents.
		extra := "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"
		if err := os.WriteFile(cfg.TLSCertFile, []byte(extra+testCA(t)), 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		cfg.TLSCertFile = filepath.Join(dir, "not-there-yet.crt")
	}
	artDir := filepath.Join(dir, "missing")
	if withArtifacts {
		artDir = filepath.Join(dir, "artifacts")
		_ = os.Mkdir(artDir, 0o700)
		for name, body := range map[string]string{
			artifacts.SetupTool:    "MZ" + strings.Repeat("\x00tool", 2000),
			artifacts.AgentWindows: "MZ" + strings.Repeat("agent", 2000),
			artifacts.Bootstrap:    "# ps1\r\n",
			"artifacts.json":       `{"product_version":"2.0.0","agent_version":"1.9.0","built_at":"2026-10-13T08:00:00Z"}`,
		} {
			if err := os.WriteFile(filepath.Join(artDir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &API{cfg: cfg, arts: artifacts.New(artDir)}
}

func goodSetup() model.SetupConfig {
	return model.SetupConfig{
		ServerURL: "https://tolikopa.corp.example.com", DomainFQDN: "corp.example.com", NetBIOS: "CORP",
		TargetDN: "OU=Workstations,DC=corp,DC=example,DC=com", GPOName: "Tolikopa Agent", DefenderExclusion: true,
	}
}

func errCode(t *testing.T, err error) (int, apierr.Body) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	return apierr.Resolve(err)
}

func TestValidateSetupConfig(t *testing.T) {
	c := goodSetup()
	c.ServerURL = " https://Tolikopa.Corp.Example.com/ "
	if err := validateSetupConfig(&c); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if c.ServerURL != "https://tolikopa.corp.example.com" {
		t.Errorf("server_url not normalised: %q", c.ServerURL)
	}
	// "Not configured yet" is a valid state.
	empty := model.SetupConfig{GPOName: defaultGPOName}
	if err := validateSetupConfig(&empty); err != nil {
		t.Errorf("unconfigured defaults rejected: %v", err)
	}

	bad := map[string]func(*model.SetupConfig){
		"server_url":  func(c *model.SetupConfig) { c.ServerURL = "http://tolikopa.corp.example.com" },
		"domain_fqdn": func(c *model.SetupConfig) { c.DomainFQDN = "corp.example.com; calc" },
		"netbios":     func(c *model.SetupConfig) { c.NetBIOS = "CORP$(x)" },
		"gpo_name":    func(c *model.SetupConfig) { c.GPOName = `x" -Evil` },
		"target_dn":   func(c *model.SetupConfig) { c.TargetDN = "OU=PCs,DC=other,DC=example,DC=com" },
	}
	for field, mutate := range bad {
		c := goodSetup()
		mutate(&c)
		status, body := errCode(t, validateSetupConfig(&c))
		if status != 400 || body.Code != apierr.InvalidRequest || body.Params["field"] != field {
			t.Errorf("%s: got %d %s %v", field, status, body.Code, body.Params)
		}
	}
	// An address is not a domain name.
	for _, ip := range []string{"10.0.0.99", "corp.123"} {
		c := goodSetup()
		c.DomainFQDN, c.TargetDN = ip, ""
		status, body := errCode(t, validateSetupConfig(&c))
		if status != 400 || body.Code != apierr.InvalidRequest || body.Params["field"] != "domain_fqdn" {
			t.Errorf("domain_fqdn %q: got %d %s %v", ip, status, body.Code, body.Params)
		}
	}
	// target_dn cannot be judged without a domain.
	c = goodSetup()
	c.DomainFQDN = ""
	if _, body := errCode(t, validateSetupConfig(&c)); body.Params["field"] != "target_dn" {
		t.Errorf("target_dn without domain: %v", body.Params)
	}
	c = goodSetup()
	c.GPOName = ""
	if _, body := errCode(t, validateSetupConfig(&c)); body.Params["field"] != "gpo_name" {
		t.Errorf("empty gpo_name: %v", body.Params)
	}
}

func TestReadiness(t *testing.T) {
	a := testAPI(t, true, true)
	r, info := a.readiness(goodSetup())
	if !(r.ServerURL && r.RootCA && r.Artifacts && r.Domain) || info == nil || info.AgentVersion != "1.9.0" {
		t.Errorf("all-ready: %+v %+v", r, info)
	}
	if m := missingOf(r, "server_url", "root_ca", "artifacts", "domain"); len(m) != 0 {
		t.Errorf("missing = %v", m)
	}

	// Fresh local checkout: nothing there. Must report, not fail.
	a = testAPI(t, false, false)
	r, info = a.readiness(model.SetupConfig{GPOName: defaultGPOName})
	if r.ServerURL || r.RootCA || r.Artifacts || r.Domain || info != nil {
		t.Errorf("nothing-ready: %+v %+v", r, info)
	}
	if m := missingOf(r, "server_url", "root_ca", "artifacts", "domain"); len(m) != 4 {
		t.Errorf("missing = %v", m)
	}
}

func TestRootCAPassesOnlyCertificates(t *testing.T) {
	a := testAPI(t, false, true)
	ca, err := a.rootCA()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ca, "PRIVATE KEY") || strings.Count(ca, "BEGIN CERTIFICATE") != 1 {
		t.Errorf("root CA output is not exactly one certificate:\n%s", ca)
	}
	if err := os.WriteFile(a.cfg.TLSCertFile, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.rootCA(); err == nil {
		t.Error("a file with no certificate was accepted")
	}
}

func TestBuildToolAndConfig(t *testing.T) {
	a := testAPI(t, true, true)

	for _, mode := range []string{bundle.ModeSetup, bundle.ModeUpdate} {
		out, err := a.buildTool(goodSetup(), mode, "tr")
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		files := map[string][]byte{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			var b bytes.Buffer
			_, _ = b.ReadFrom(rc)
			rc.Close()
			files[f.Name] = b.Bytes()
		}
		var m bundle.Manifest
		if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
			t.Fatal(err)
		}
		if m.Mode != mode || m.Lang != "tr" || m.AgentVersion != "1.9.0" || m.DomainFQDN != "corp.example.com" {
			t.Errorf("%s manifest: %+v", mode, m)
		}
		if mode == bundle.ModeUpdate && (m.GPOName != "" || m.TargetDN != "" || m.DefenderExclusion) {
			t.Errorf("update manifest carries GPO fields: %+v", m)
		}
		// The token is in config.json and nowhere else in the download.
		if bytes.Contains(files["manifest.json"], []byte(apiTestToken)) {
			t.Error("ingest token in the manifest")
		}
		var conf map[string]any
		if err := json.Unmarshal(files["config.json"], &conf); err != nil {
			t.Fatal(err)
		}
		if conf["ingest_token"] != apiTestToken || conf["backend_url"] != "https://tolikopa.corp.example.com" {
			t.Errorf("config.json: %v", conf)
		}
		if pemText, _ := conf["tls_ca_pem"].(string); !strings.HasPrefix(pemText, "-----BEGIN CERTIFICATE-----") {
			t.Errorf("tls_ca_pem: %q", conf["tls_ca_pem"])
		}
		for _, k := range []string{"allow_app_removal", "allow_startup_control", "enforce_actions", "hygiene_hours"} {
			if _, has := conf[k]; has {
				t.Errorf("bundled config.json sets the kill switch %q", k)
			}
		}
	}

	// Standalone config.json is the same document.
	raw, err := a.buildAgentConfig(goodSetup())
	if err != nil || !bytes.Contains(raw, []byte(apiTestToken)) {
		t.Fatalf("buildAgentConfig: %v", err)
	}
}

func TestBuildToolFailureModes(t *testing.T) {
	// No artifacts → 503-class error regardless of the rest.
	a := testAPI(t, false, true)
	if _, err := a.buildTool(goodSetup(), bundle.ModeSetup, "en"); err == nil || !strings.Contains(err.Error(), artifacts.ErrUnavailable.Error()) {
		t.Errorf("without artifacts: %v", err)
	}

	// Artifacts present, config + CA missing → 409 setup_incomplete listing them.
	a = testAPI(t, true, false)
	_, err := a.buildTool(model.SetupConfig{GPOName: defaultGPOName}, bundle.ModeSetup, "en")
	status, body := errCode(t, err)
	missing, _ := body.Params["missing"].([]string)
	if status != 409 || body.Code != apierr.SetupIncomplete || strings.Join(missing, ",") != "server_url,root_ca,domain" {
		t.Errorf("got %d %s %v", status, body.Code, body.Params)
	}

	// config.json alone needs no domain and no artifacts.
	a = testAPI(t, false, false)
	_, err = a.buildAgentConfig(goodSetup())
	_, body = errCode(t, err)
	if missing, _ := body.Params["missing"].([]string); strings.Join(missing, ",") != "root_ca" {
		t.Errorf("config.json without CA: %v", body.Params)
	}

	// A stored value the tool would refuse never produces a download.
	a = testAPI(t, true, true)
	c := goodSetup()
	c.GPOName = "$(calc)"
	_, err = a.buildTool(c, bundle.ModeSetup, "en")
	status, body = errCode(t, err)
	if status != 409 || body.Code != apierr.SetupIncomplete {
		t.Errorf("invalid stored gpo_name: %d %s", status, body.Code)
	}
}

func TestNormVersion(t *testing.T) {
	if normVersion(" v1.9.0 ") != normVersion("1.9.0") || normVersion("1.9.0") == normVersion("1.8.2") {
		t.Error("normVersion")
	}
}

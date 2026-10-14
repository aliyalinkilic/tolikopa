// Package bundle builds the installation-specific downloads of the rollout
// tools: a zip holding the unmodified, generic tolikopa-setup.exe next to
// tolikopa.bundle, the installation-specific archive (manifest + files).
//
// The contract is owned by the setup tool (setup/internal/payload, a separate Go
// module that this one must not import). Manifest and the validation below are a
// hand-kept MIRROR of payload.Manifest / payload.Validate: the tool decodes the
// manifest with DisallowUnknownFields and re-validates every field, so anything
// this package emits that the tool would refuse is a download that fails on an
// administrator's PC. TestManifestJSONFields pins the field names; keep the
// regexes byte-for-byte identical to the tool's.
//
// What never goes through here: domain credentials. The server holds none; the
// tool runs with the rights of the administrator who started it.
package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/artifacts"
)

// Modes of the tool.
const (
	ModeSetup  = "setup"  // copy files + create and link the GPO
	ModeUpdate = "update" // replace the files on the share, GPO untouched
)

// Names of the files inside a bundle (the tool's closed allowlist).
const (
	FileAgent     = "tolikopa-agent.exe"
	FileBootstrap = "bootstrap.ps1"
	FileConfig    = "config.json"
	manifestName  = "manifest.json"
)

// File is one bundled file and its SHA-256.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Manifest mirrors payload.Manifest in the setup module — same fields, same
// JSON names, same order.
type Manifest struct {
	Mode              string `json:"mode"`
	Lang              string `json:"lang"`
	ProductVersion    string `json:"product_version"`
	AgentVersion      string `json:"agent_version"`
	ServerURL         string `json:"server_url"`
	DomainFQDN        string `json:"domain_fqdn"`
	NetBIOS           string `json:"netbios"`
	TargetDN          string `json:"target_dn"`
	GPOName           string `json:"gpo_name"`
	DefenderExclusion bool   `json:"defender_exclusion"`
	Files             []File `json:"files"`
}

var allowedFiles = map[string]bool{FileAgent: true, FileBootstrap: true, FileConfig: true}

// Mirrors of the tool's expressions. The DN form is deliberately narrower than
// RFC 4514 (no quotes, escapes, '$', ';' or backticks): the tool hands the value
// to PowerShell as an argument.
var (
	reFQDN    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)
	reNetBIOS = regexp.MustCompile(`^[A-Za-z0-9-]{1,15}$`)
	reGPOName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$`)
	reSHA     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,31}$`)
	reDN      = regexp.MustCompile(`^(OU|DC|CN)=[\p{L}\p{N} ._&()-]+(,(OU|DC|CN)=[\p{L}\p{N} ._&()-]+)*$`)

	reHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	rePort = regexp.MustCompile(`^[0-9]{1,5}$`)
)

// FieldError names the offending field of a config or manifest.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(field, msg string) error { return &FieldError{Field: field, Msg: msg} }

// ValidFQDN reports whether s is an acceptable AD domain name.
//
// A name whose LAST label is all digits is refused: "10.0.0.99" and "corp.123"
// match the label grammar but are an address (or address-like), not a domain,
// and the value ends up in a UNC path (\\<fqdn>\NETLOGON). Mirrors numericTLD in
// the setup tool; the regex itself is unchanged.
func ValidFQDN(s string) bool {
	return reFQDN.MatchString(s) && len(s) <= 253 && !numericTLD(s)
}

// numericTLD reports whether the last dot-separated label of s consists only of
// digits.
func numericTLD(s string) bool {
	last := s[strings.LastIndexByte(s, '.')+1:]
	if last == "" {
		return false
	}
	for i := 0; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			return false
		}
	}
	return true
}

// ValidNetBIOS reports whether s is an acceptable NetBIOS domain name.
func ValidNetBIOS(s string) bool { return reNetBIOS.MatchString(s) }

// ValidGPOName reports whether s is an acceptable GPO display name.
func ValidGPOName(s string) bool { return reGPOName.MatchString(s) }

// DomainDN converts corp.example.com to DC=corp,DC=example,DC=com.
func DomainDN(fqdn string) string {
	parts := strings.Split(fqdn, ".")
	for i, p := range parts {
		parts[i] = "DC=" + p
	}
	return strings.Join(parts, ",")
}

// ValidTargetDN reports whether dn is an acceptable GPO link target inside the
// domain fqdn. The empty string (= domain root) is valid.
func ValidTargetDN(dn, fqdn string) bool {
	if dn == "" {
		return true
	}
	if !reDN.MatchString(dn) || len(dn) > 512 {
		return false
	}
	return strings.HasSuffix(strings.ToLower(dn), strings.ToLower(DomainDN(fqdn)))
}

// NormalizeServerURL validates the URL agents will use as backend_url and
// returns it in canonical form (https://host[:port], no path). It must be https:
// agents send the ingest token to it.
func NormalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 255 {
		return "", errors.New("too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("not a valid URL")
	}
	if u.Scheme != "https" {
		return "", errors.New("must start with https://")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("must be only https://host[:port]")
	}
	host := u.Hostname()
	if !reHost.MatchString(host) || len(host) > 253 {
		return "", errors.New("invalid host")
	}
	out := "https://" + strings.ToLower(host)
	if p := u.Port(); p != "" {
		if !rePort.MatchString(p) {
			return "", errors.New("invalid port")
		}
		if p != "443" {
			out += ":" + p
		}
	}
	return out, nil
}

// Validate is the mirror of payload.Manifest.Validate. A manifest that passes
// here is one the tool accepts.
func (m *Manifest) Validate() error {
	if m.Mode != ModeSetup && m.Mode != ModeUpdate {
		return fieldErr("mode", "unknown mode")
	}
	if !ValidFQDN(m.DomainFQDN) {
		return fieldErr("domain_fqdn", "invalid domain name")
	}
	if m.NetBIOS != "" && !ValidNetBIOS(m.NetBIOS) {
		return fieldErr("netbios", "invalid NetBIOS name")
	}
	if m.Mode == ModeSetup {
		if !ValidGPOName(m.GPOName) {
			return fieldErr("gpo_name", "invalid GPO name")
		}
		if !ValidTargetDN(m.TargetDN, m.DomainFQDN) {
			return fieldErr("target_dn", "invalid or outside the domain")
		}
	}
	if m.AgentVersion != "" && !reVersion.MatchString(m.AgentVersion) {
		return fieldErr("agent_version", "invalid version")
	}
	if len(m.Files) == 0 {
		return fieldErr("files", "no files")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !allowedFiles[f.Name] || seen[f.Name] {
			return fieldErr("files", "unexpected file "+f.Name)
		}
		if !reSHA.MatchString(f.SHA256) {
			return fieldErr("files", "bad sha256 for "+f.Name)
		}
		seen[f.Name] = true
	}
	if m.Mode == ModeSetup {
		for name := range allowedFiles {
			if !seen[name] {
				return fieldErr("files", "setup bundle is missing "+name)
			}
		}
	}
	return nil
}

// Content is one file to put in a bundle.
type Content struct {
	Name string
	Data []byte
}

// Archive returns the bundle: a standalone zip archive holding manifest.json and
// the given files (served inside the download as tolikopa.bundle). m.Files is
// filled in from the contents (name + SHA-256), then the manifest is validated —
// an invalid one is never emitted.
func Archive(m Manifest, files []Content) ([]byte, error) {
	m.Files = make([]File, 0, len(files))
	for _, f := range files {
		m.Files = append(m.Files, File{Name: f.Name, SHA256: artifacts.SumBytes(f.Data)})
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return writeZip(append([]Content{{Name: manifestName, Data: mj}}, files...))
}

// Names inside a download zip.
const (
	BundleName = "tolikopa.bundle" // the setup tool looks for exactly this name next to itself
	ReadmeName = "README.txt"
)

// Readme is the README.txt of every download zip (CRLF: it is read in Notepad).
var Readme = []byte(strings.ReplaceAll(`ToliKOPA domain setup tool
==========================

1. Extract ALL files of this zip into the same folder.
2. Run the .exe from that folder. It reads its configuration from
   tolikopa.bundle, which must stay next to it.

tolikopa.bundle carries this installation's configuration and the agents'
ingest token. Keep it private and delete it (and this folder) after use.

The .exe is identical for every installation of the same ToliKOPA build;
only tolikopa.bundle is specific to yours.

--------------------------------------------------------------------------

ToliKOPA etki alanı kurulum aracı
=================================

1. Bu zip'teki TÜM dosyaları aynı klasöre çıkarın.
2. .exe dosyasını o klasörden çalıştırın. Yapılandırmasını yanındaki
   tolikopa.bundle dosyasından okur; bu dosya exe ile aynı klasörde kalmalıdır.

tolikopa.bundle bu kurulumun yapılandırmasını ve ajanların ingest token'ını
taşır. Kimseyle paylaşmayın; işiniz bittikten sonra (klasörle birlikte) silin.

.exe dosyası aynı ToliKOPA derlemesinin tüm kurulumlarında aynıdır; yalnızca
tolikopa.bundle sizin kurulumunuza özeldir.
`, "\n", "\r\n"))

// Package returns the download zip: the setup tool executable exactly as built
// (byte for byte, so its hash is that of the artifact for every
// installation), the bundle archive and README.txt. exeName is the name the
// executable gets inside the zip (tolikopa-setup.exe or tolikopa-update.exe).
//
// Nothing is appended to the executable any more: a PE with a zip overlay is
// the classic dropper layout and per-download hashes can never build reputation
// with SmartScreen or Microsoft Defender.
func Package(exeName string, exe, archive []byte) ([]byte, error) {
	if len(exe) == 0 {
		return nil, errors.New("bundle: empty executable")
	}
	if len(archive) == 0 {
		return nil, errors.New("bundle: empty bundle")
	}
	return writeZip([]Content{
		{Name: exeName, Data: exe},
		{Name: BundleName, Data: archive},
		{Name: ReadmeName, Data: Readme},
	})
}

func writeZip(files []Content) ([]byte, error) {
	size := 1024
	for _, f := range files {
		size += len(f.Data)
	}
	var buf bytes.Buffer
	buf.Grow(size)
	zw := zip.NewWriter(&buf)
	now := time.Now()
	for _, f := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return nil, fmt.Errorf("bundle: %s: %w", f.Name, err)
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, fmt.Errorf("bundle: %s: %w", f.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── agent config.json ────────────────────────────────────────────────────────

// AgentIntervals mirrors the agent's intervals block.
type AgentIntervals struct {
	VitalsSec int `json:"vitals_sec"`
	SampleSec int `json:"sample_sec"`
	PostSec   int `json:"post_sec"`
	SmartMin  int `json:"smart_min"`
}

// AgentConfig is the config.json the server generates for agents.
//
// It deliberately has NO field for allow_app_removal, allow_startup_control,
// enforce_actions or hygiene_hours. Those are the local kill switches of the
// endpoint-hygiene feature (CLAUDE.md "Safety", invariant 4): they default to
// off, live only in the config.json on the domain's share, and can never be set
// by the server. The setup tool asks the administrator locally and preserves
// whatever the share's existing config.json says. Do not add them here —
// TestAgentConfigNeverCarriesKillSwitches fails if anyone does.
type AgentConfig struct {
	BackendURL  string         `json:"backend_url"`
	IngestToken string         `json:"ingest_token"`
	TLSCAPem    string         `json:"tls_ca_pem"`
	Intervals   AgentIntervals `json:"intervals"`
	TopN        int            `json:"top_n"`
	SpoolMaxMB  int            `json:"spool_max_mb"`
	LogLevel    string         `json:"log_level"`
}

// NewAgentConfig returns the config with the documented non-sensitive defaults
// (the same values as deploy/gpo/config.example.json).
func NewAgentConfig(serverURL, ingestToken, caPEM string) AgentConfig {
	return AgentConfig{
		BackendURL:  serverURL,
		IngestToken: ingestToken,
		TLSCAPem:    caPEM,
		Intervals:   AgentIntervals{VitalsSec: 15, SampleSec: 60, PostSec: 60, SmartMin: 45},
		TopN:        10,
		SpoolMaxMB:  50,
		LogLevel:    "info",
	}
}

// JSON renders the config as the file the agent reads.
func (c AgentConfig) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

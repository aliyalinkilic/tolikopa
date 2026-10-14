// Package payload reads the deployment bundle the server attaches to this tool.
//
// The server serves one generic tolikopa-setup.exe with a zip archive appended to
// it (archive/zip locates the central directory from the end of the file, so a zip
// with an executable in front of it opens normally). The archive holds
// manifest.json plus the files that go to the NETLOGON share. Nothing in it is
// executed by this tool: the files are copied, and the manifest is validated
// field by field before any value is used.
package payload

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Mode selects what the tool does with the bundle.
const (
	ModeSetup  = "setup"  // copy files + create and link the GPO
	ModeUpdate = "update" // replace the files on the share, GPO untouched
)

// ShareDir is the folder under NETLOGON. It is a constant, never manifest-driven.
const ShareDir = "Tolikopa"

// File is one bundled file and the hash the server computed for it.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Manifest describes the deployment. Every field comes from the server and is
// therefore untrusted until Validate has passed.
type Manifest struct {
	Mode           string `json:"mode"`
	Lang           string `json:"lang"`
	ProductVersion string `json:"product_version"`
	AgentVersion   string `json:"agent_version"`
	ServerURL      string `json:"server_url"`
	DomainFQDN     string `json:"domain_fqdn"`
	NetBIOS        string `json:"netbios"`
	// TargetDN is where the GPO is linked. Empty means the domain root.
	TargetDN string `json:"target_dn"`
	GPOName  string `json:"gpo_name"`
	// DefenderExclusion adds a Microsoft Defender path exclusion for the installed
	// agent binary to the GPO, so an unsigned LocalSystem service is not quarantined.
	DefenderExclusion bool   `json:"defender_exclusion"`
	Files             []File `json:"files"`
}

// allowedFiles is the closed set of names that may be written to the share.
var allowedFiles = map[string]bool{"tolikopa-agent.exe": true, "bootstrap.ps1": true, "config.json": true}

var (
	reFQDN    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)
	reNetBIOS = regexp.MustCompile(`^[A-Za-z0-9-]{1,15}$`)
	reGPOName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$`)
	reSHA     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,31}$`)
	// A distinguished name made of OU=/DC=/CN= parts. Deliberately narrower than
	// RFC 4514: no quotes, escapes, '$', ';' or backticks, because the value is
	// handed to PowerShell as an argument.
	reDN = regexp.MustCompile(`^(OU|DC|CN)=[\p{L}\p{N} ._&()-]+(,(OU|DC|CN)=[\p{L}\p{N} ._&()-]+)*$`)
)

// Validate rejects any manifest whose values fall outside the expected shapes.
func (m *Manifest) Validate() error {
	if m.Mode != ModeSetup && m.Mode != ModeUpdate {
		return fmt.Errorf("manifest: unknown mode %q", m.Mode)
	}
	if !reFQDN.MatchString(m.DomainFQDN) || len(m.DomainFQDN) > 253 || numericTLD(m.DomainFQDN) {
		return fmt.Errorf("manifest: invalid domain_fqdn %q", m.DomainFQDN)
	}
	if m.NetBIOS != "" && !reNetBIOS.MatchString(m.NetBIOS) {
		return fmt.Errorf("manifest: invalid netbios %q", m.NetBIOS)
	}
	if m.Mode == ModeSetup {
		if !reGPOName.MatchString(m.GPOName) {
			return fmt.Errorf("manifest: invalid gpo_name %q", m.GPOName)
		}
		if m.TargetDN != "" && (!reDN.MatchString(m.TargetDN) || len(m.TargetDN) > 512) {
			return fmt.Errorf("manifest: invalid target_dn %q", m.TargetDN)
		}
		if m.TargetDN != "" && !strings.HasSuffix(strings.ToLower(m.TargetDN), strings.ToLower(m.DomainDN())) {
			return fmt.Errorf("manifest: target_dn %q is not inside %s", m.TargetDN, m.DomainFQDN)
		}
	}
	// Both are only printed, but printed to the admin who decides whether to go
	// on, so they must not carry terminal escapes or a misleading look-alike.
	if m.AgentVersion != "" && !reVersion.MatchString(m.AgentVersion) {
		return fmt.Errorf("manifest: invalid agent_version %q", m.AgentVersion)
	}
	if m.ServerURL != "" && !validServerURL(m.ServerURL) {
		return fmt.Errorf("manifest: invalid server_url %q", m.ServerURL)
	}
	if len(m.Files) == 0 {
		return errors.New("manifest: no files")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !allowedFiles[f.Name] || seen[f.Name] {
			return fmt.Errorf("manifest: unexpected file %q", f.Name)
		}
		if !reSHA.MatchString(f.SHA256) {
			return fmt.Errorf("manifest: bad sha256 for %s", f.Name)
		}
		seen[f.Name] = true
	}
	if m.Mode == ModeSetup {
		for name := range allowedFiles {
			if !seen[name] {
				return fmt.Errorf("manifest: setup bundle is missing %s", name)
			}
		}
	}
	return nil
}

// ValidHostName reports whether s is a DNS name the tool may put in a DNS record:
// at least two labels, no numeric top-level label (that would be an IP address).
func ValidHostName(s string) bool {
	return len(s) <= 253 && reFQDN.MatchString(s) && !numericTLD(s)
}

// ValidIPv4 reports whether s is a dotted-quad IPv4 address that can be a host:
// not unspecified, loopback, multicast or broadcast.
func ValidIPv4(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil || strings.Contains(s, ":") {
		return false
	}
	return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsMulticast() && !ip.Equal(net.IPv4bcast)
}

// validServerURL accepts a plain http(s)://host[:port][/path] of printable ASCII,
// without credentials, query or fragment.
func validServerURL(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return false
	}
	return u.User == nil && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}

// numericTLD reports whether the last label is all digits. That is an IP address
// (or something shaped like one), never an Active Directory domain, and the tool
// must not be steerable towards an arbitrary host.
func numericTLD(fqdn string) bool {
	last := fqdn[strings.LastIndex(fqdn, ".")+1:]
	for _, r := range last {
		if r < '0' || r > '9' {
			return false
		}
	}
	return last != ""
}

// DomainDN converts corp.example.com to DC=corp,DC=example,DC=com.
func (m *Manifest) DomainDN() string {
	parts := strings.Split(m.DomainFQDN, ".")
	for i, p := range parts {
		parts[i] = "DC=" + p
	}
	return strings.Join(parts, ",")
}

// LinkTarget is TargetDN, or the domain root when none was chosen.
func (m *Manifest) LinkTarget() string {
	if m.TargetDN != "" {
		return m.TargetDN
	}
	return m.DomainDN()
}

// SharePath is the UNC folder the files are copied to.
func (m *Manifest) SharePath() string {
	return `\\` + m.DomainFQDN + `\NETLOGON\` + ShareDir
}

// Bundle is an opened payload archive.
type Bundle struct {
	Manifest Manifest
	zr       *zip.ReadCloser
	entries  map[string]*zip.File
}

// Open reads the archive at path (normally the running executable itself).
func Open(path string) (*Bundle, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("no deployment bundle found in %s: %w", path, err)
	}
	b := &Bundle{zr: zr, entries: map[string]*zip.File{}}
	for _, f := range zr.File {
		b.entries[f.Name] = f
	}
	mf, ok := b.entries["manifest.json"]
	if !ok {
		zr.Close()
		return nil, errors.New("bundle has no manifest.json")
	}
	rc, err := mf.Open()
	if err != nil {
		zr.Close()
		return nil, err
	}
	defer rc.Close()
	dec := json.NewDecoder(io.LimitReader(rc, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b.Manifest); err != nil {
		zr.Close()
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if err := b.Manifest.Validate(); err != nil {
		zr.Close()
		return nil, err
	}
	return b, nil
}

// Close releases the archive.
func (b *Bundle) Close() error { return b.zr.Close() }

// maxFileSize bounds a single extracted file (the agent is ~8 MB).
const maxFileSize = 128 << 20

// Read returns a bundled file's bytes after checking them against the manifest
// hash, so a truncated or tampered download fails here and never reaches the share.
func (b *Bundle) Read(f File) ([]byte, error) {
	e, ok := b.entries[f.Name]
	if !ok {
		return nil, fmt.Errorf("bundle is missing %s", f.Name)
	}
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("%s is larger than expected", f.Name)
	}
	if got := Sum(data); got != f.SHA256 {
		return nil, fmt.Errorf("%s: checksum mismatch (bundle is corrupt)", f.Name)
	}
	return data, nil
}

// Sum is the lowercase hex SHA-256 of data.
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

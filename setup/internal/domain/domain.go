// Package domain performs the Active Directory side of a deployment: publishing
// the bundle to NETLOGON, the panel's DNS record, and creating/linking the GPO
// (which also makes domain PCs trust the panel's certificate).
//
// It runs under the identity of whoever started the tool. No credential is read,
// stored or forwarded: the admin's own Kerberos session is what authorises every
// write, so the Tolikopa server never holds domain rights.
//
// PowerShell is used here on purpose (the GroupPolicy module is the supported way
// to create a GPO). That is specific to this interactive admin tool; the agent
// that runs on endpoints never starts a shell.
package domain

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/aliyalinkilic/tolikopa/setup/internal/payload"
)

//go:embed gpo.ps1
var gpoScript []byte

//go:embed task.xml.tmpl
var taskTemplate string

// Result mirrors the JSON object gpo.ps1 prints on its last line.
type Result struct {
	OK                   bool   `json:"ok"`
	Action               string `json:"action"`
	Error                string `json:"error"`
	Computer             string `json:"computer"`
	PartOfDomain         bool   `json:"part_of_domain"`
	ComputerDomain       string `json:"computer_domain"`
	User                 string `json:"user"`
	GPModule             bool   `json:"gp_module"`
	ShareParentReachable bool   `json:"share_parent_reachable"`
	ShareExists          bool   `json:"share_exists"`
	GPOExists            bool   `json:"gpo_exists"`
	GPOForeign           bool   `json:"gpo_foreign"`
	DomainMatch          bool   `json:"domain_match"`
	GPOID                string `json:"gpo_id"`
	GPOCreated           bool   `json:"gpo_created"`
	GPORemoved           bool   `json:"gpo_removed"`
	TaskUpdated          bool   `json:"task_updated"`
	LinkCreated          bool   `json:"link_created"`
	DefenderExclusion    bool   `json:"defender_exclusion"`
	Target               string `json:"target"`
	Version              int    `json:"version"`

	// DNS (preflight and the dns action).
	DNSModule   bool     `json:"dns_module"`
	DNSZone     string   `json:"dns_zone"`
	DNSState    string   `json:"dns_state"` // add | present | new_zone | conflict | unknown
	DNSExisting []string `json:"dns_existing"`
	DNSError    string   `json:"dns_error"`

	// Certificate trust (apply).
	TrustInstalled bool `json:"trust_installed"`
}

// DNSTarget is the record the panel's name should resolve through.
type DNSTarget struct {
	Name string
	IP   string
}

const resultMarker = "##RESULT##"

// run executes the embedded script with the given action and extra parameters.
// Parameters are passed as separate argv entries after -File, which PowerShell
// binds literally; the manifest validation in package payload is what guarantees
// they contain nothing but names.
func run(m *payload.Manifest, action string, extra ...string) (*Result, error) {
	dir, err := os.MkdirTemp("", "tolikopa-setup-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "gpo.ps1")
	if err := os.WriteFile(script, gpoScript, 0o600); err != nil {
		return nil, err
	}
	args := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Action", action, "-DomainFqdn", m.DomainFQDN,
	}
	if m.GPOName != "" {
		args = append(args, "-GpoName", m.GPOName)
	}
	if m.TargetDN != "" {
		args = append(args, "-TargetDn", m.TargetDN)
	}
	args = append(args, extra...)

	cmd := exec.Command(powershellPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, resultMarker) {
			var r Result
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, resultMarker)), &r); err != nil {
				return nil, fmt.Errorf("unreadable result from PowerShell: %w", err)
			}
			return &r, nil
		}
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" && runErr != nil {
		msg = runErr.Error()
	}
	return nil, fmt.Errorf("PowerShell did not complete: %s", msg)
}

// powershellPath is Windows PowerShell under the system directory, never whatever
// "powershell.exe" resolves to on PATH.
func powershellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// Preflight runs the read-only checks. dns is nil when no record is wanted.
func Preflight(m *payload.Manifest, dns *DNSTarget) (*Result, error) {
	extra := []string{"-SharePath", m.SharePath()}
	if dns != nil {
		if !payload.ValidHostName(dns.Name) || !payload.ValidIPv4(dns.IP) {
			return nil, errors.New("invalid DNS name or address")
		}
		extra = append(extra, "-DnsName", dns.Name, "-DnsIp", dns.IP)
	}
	return run(m, "preflight", extra...)
}

// ApplyDNS makes dns.Name resolve to dns.IP on the domain's DNS servers: an A
// record in the zone that contains the name, or — when no zone does and
// allowNewZone is set — a new AD-integrated zone of that exact name. A new zone
// answers for a whole name space on every domain PC, so it is created only after
// the admin confirmed it interactively; without that the script refuses
// (dns_new_zone_not_confirmed), even if a zone vanished since the preflight. An
// existing record pointing elsewhere is never overwritten.
func ApplyDNS(m *payload.Manifest, dns DNSTarget, allowNewZone bool) (*Result, error) {
	if !payload.ValidHostName(dns.Name) || !payload.ValidIPv4(dns.IP) {
		return nil, errors.New("invalid DNS name or address")
	}
	newZone := "deny"
	if allowNewZone {
		newZone = "allow"
	}
	return run(m, "dns", "-DnsName", dns.Name, "-DnsIp", dns.IP, "-NewZone", newZone)
}

// TaskXML renders the Group Policy Preferences scheduled-task definition.
func TaskXML(m *payload.Manifest) ([]byte, error) {
	t, err := template.New("task").Parse(taskTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	// uid and changed are derived from the domain, not from the clock: a re-run must
	// produce the same bytes, or every run would bump the GPO version for nothing.
	err = t.Execute(&buf, map[string]string{
		"Changed":   "2026-01-01 00:00:00",
		"UID":       stableGUID("TolikopaAgent|" + strings.ToLower(m.DomainFQDN)),
		"Bootstrap": m.SharePath() + `\bootstrap.ps1`,
	})
	return buf.Bytes(), err
}

// ApplyGPO creates or updates the GPO and links it to the target. With a
// trustable cert the same GPO also places it in the computers' trusted root
// store; without one, any certificate an earlier run placed there is removed.
func ApplyGPO(m *payload.Manifest, cert *PanelCert) (*Result, error) {
	xml, err := TaskXML(m)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "tolikopa-task-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "ScheduledTasks.xml")
	if err := os.WriteFile(path, xml, 0o600); err != nil {
		return nil, err
	}
	exclusion := "off"
	if m.DefenderExclusion {
		exclusion = "on"
	}
	extra := []string{"-TaskXml", path, "-DefenderExclusion", exclusion}
	if cert != nil && cert.Trustable {
		certPath := filepath.Join(dir, "panel.cer")
		if err := os.WriteFile(certPath, cert.DER, 0o600); err != nil {
			return nil, err
		}
		extra = append(extra, "-TrustCert", certPath, "-TrustThumb", cert.Thumbprint)
	}
	return run(m, "apply", extra...)
}

// RemoveGPO unlinks and deletes the GPO.
func RemoveGPO(m *payload.Manifest) (*Result, error) { return run(m, "remove") }

// PublishOrder puts tolikopa-agent.exe last: bootstrap.ps1 and config.json are in place
// before the binary whose hash change makes every PC act.
var PublishOrder = []string{"config.json", "bootstrap.ps1", "tolikopa-agent.exe"}

// Publish writes the bundle's files to the NETLOGON share. Each file goes to a
// temporary name and is renamed into place, so a PC that refreshes policy
// mid-copy never reads half an executable. It returns the names that changed.
// config.json is not copied verbatim: see MergeConfig.
func Publish(b *payload.Bundle, sw Switches) ([]string, error) {
	m := &b.Manifest
	share := m.SharePath()
	if err := os.MkdirAll(share, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", share, err)
	}
	// Lock the folder down before the first file lands in it: config.json carries
	// the agents' ingest token, and NETLOGON is readable by every domain user.
	if res, err := run(m, "protect", "-SharePath", share); err != nil {
		return nil, err
	} else if !res.OK {
		return nil, fmt.Errorf("cannot restrict access to %s: %s", share, res.Error)
	}
	byName := map[string]payload.File{}
	for _, f := range m.Files {
		byName[f.Name] = f
	}
	var changed []string
	for _, name := range PublishOrder {
		f, ok := byName[name]
		if !ok {
			continue
		}
		dest := filepath.Join(share, name)
		cur, curErr := os.ReadFile(dest)
		if name != "config.json" && curErr == nil && payload.Sum(cur) == f.SHA256 {
			continue // already current
		}
		data, err := b.Read(f)
		if err != nil {
			return changed, err
		}
		if name == "config.json" {
			if data, err = MergeConfig(data, cur, sw); err != nil {
				return changed, err
			}
			if curErr == nil && bytes.Equal(cur, data) {
				continue
			}
		}
		want := payload.Sum(data)
		tmp := dest + ".new"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return changed, fmt.Errorf("cannot write %s: %w", tmp, err)
		}
		written, err := os.ReadFile(tmp)
		if err != nil || payload.Sum(written) != want {
			os.Remove(tmp)
			return changed, fmt.Errorf("%s: verification after copy failed", name)
		}
		if err := os.Rename(tmp, dest); err != nil {
			os.Remove(tmp)
			return changed, fmt.Errorf("cannot replace %s: %w", dest, err)
		}
		changed = append(changed, name)
	}
	return changed, nil
}

// RemoveShare deletes the published folder.
func RemoveShare(m *payload.Manifest) error {
	share := m.SharePath()
	if !strings.HasSuffix(share, `\NETLOGON\`+payload.ShareDir) {
		return errors.New("refusing to remove an unexpected path")
	}
	return os.RemoveAll(share)
}

// stableGUID derives a GUID-shaped identifier from a name.
func stableGUID(name string) string {
	b := sha256.Sum256([]byte(name))
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("{%x-%x-%x-%x-%x}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

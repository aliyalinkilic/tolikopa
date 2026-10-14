// Package config loads and persists the agent configuration and resolves the
// per-OS state directories (config.json, agent_id, spool, logs).
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Intervals controls collection and post cadence (all values in the indicated unit).
type Intervals struct {
	VitalsSec int `json:"vitals_sec"` // light cpu/mem/net sampling cadence
	SampleSec int `json:"sample_sec"` // full sample (disks + top-N processes) cadence
	PostSec   int `json:"post_sec"`   // batch POST / heartbeat cadence
	SmartMin  int `json:"smart_min"`  // SMART/WMI health cadence (minutes)
}

// Config is the on-disk agent configuration (config.json).
type Config struct {
	BackendURL  string    `json:"backend_url"`
	IngestToken string    `json:"ingest_token"`
	TLSCAPem    string    `json:"tls_ca_pem"` // pin internal CA; empty => system trust store
	Intervals   Intervals `json:"intervals"`
	TopN        int       `json:"top_n"`
	SpoolMaxMB  int       `json:"spool_max_mb"`
	// IdleThresholdSec is the seconds of no keyboard/mouse input after which an
	// unlocked console session is reported as "idle" instead of "active".
	// Default 300; values below 60 are clamped to 60. Windows only (the idle
	// signal comes from the in-session helper); ignored elsewhere.
	IdleThresholdSec int    `json:"idle_threshold_sec"`
	LogLevel         string `json:"log_level"`

	// ── Endpoint hygiene kill switches (GPO-provisioned) ──────────────────────
	//
	// All three default to FALSE and applyDefaults() never flips them on: a zero
	// value is a deliberate "no". The server can select items from the compiled
	// catalog, but it can NEVER widen these — they are the local veto. Inventory
	// (which catalog items are present) is always collected and is read-only;
	// only ACTUATION is gated here.

	// AllowAppRemoval permits removing/deprovisioning catalog Appx packages.
	AllowAppRemoval bool `json:"allow_app_removal"`
	// AllowStartupControl permits disabling/enabling catalog logon (Run) entries.
	AllowStartupControl bool `json:"allow_startup_control"`
	// EnforceActions=false means evaluate the policy and report what WOULD happen
	// (outcome "dry_run"), never actuate. It is independent of the two switches
	// above and of the server's dry_run flag — the most restrictive one wins.
	EnforceActions bool `json:"enforce_actions"`
	// HygieneHours is the hygiene inventory/reconcile cadence in hours
	// (default 6, floor 1).
	HygieneHours int `json:"hygiene_hours"`
}

// Default returns a Config populated with the documented defaults.
func Default() *Config {
	return &Config{
		BackendURL: "https://tolikopa.local",
		Intervals: Intervals{
			VitalsSec: 15,
			SampleSec: 60,
			PostSec:   60,
			SmartMin:  45,
		},
		TopN:             10,
		SpoolMaxMB:       50,
		IdleThresholdSec: 300,
		LogLevel:         "info",
		HygieneHours:     6,
		// AllowAppRemoval / AllowStartupControl / EnforceActions stay false on
		// purpose: hygiene actuation is opt-in per GPO, never by default.
	}
}

// baseDir is the agent's data root: C:\ProgramData\Tolikopa on Windows,
// /var/lib/tolikopa on Linux.
func baseDir() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Tolikopa")
	}
	return "/var/lib/tolikopa"
}

// ConfigPath returns the location of config.json.
// Windows: C:\ProgramData\Tolikopa\config.json ; Linux: /etc/tolikopa/config.json.
func ConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(baseDir(), "config.json")
	}
	return "/etc/tolikopa/config.json"
}

// StateDir returns the directory holding agent_id and other small state files.
func StateDir() string { return filepath.Join(baseDir(), "state") }

// SpoolDir returns the directory holding spooled (undelivered) NDJSON batches.
func SpoolDir() string { return filepath.Join(baseDir(), "spool") }

// LogDir returns the directory holding agent logs.
func LogDir() string { return filepath.Join(baseDir(), "logs") }

// AgentIDPath returns the location of the persisted stable agent UUID.
func AgentIDPath() string { return filepath.Join(StateDir(), "agent_id") }

// EnsureDirs creates the state, spool and log directories if missing.
func EnsureDirs() error {
	for _, d := range []string{StateDir(), SpoolDir(), LogDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// Load reads config.json from ConfigPath, filling any zero-valued field with its
// default. A missing file is not an error: defaults are returned so the agent can
// still run (and heartbeat) until GPO drops a real config in place.
func Load() (*Config, error) {
	cfg := Default()
	b, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := rejectLookalikeKeys(b); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := ValidateBackendURL(cfg.BackendURL); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ValidateBackendURL refuses any backend_url that is not an absolute https://
// URL with a host. Every request carries the ingest token as a bearer header,
// so an http:// URL — whether mistyped or planted in an edited config.json —
// would hand the token (and the telemetry) to anyone on the path in cleartext.
// There is deliberately no opt-out. Userinfo is refused too: credentials have
// no place in this URL and would end up in logs.
func ValidateBackendURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("config: backend_url is not a valid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("config: backend_url must use https:// (got scheme %q); plain http would send the ingest token in cleartext", u.Scheme)
	}
	if u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("config: backend_url has no host")
	}
	if u.User != nil {
		return fmt.Errorf("config: backend_url must not contain user credentials")
	}
	return nil
}

// switchKeys are the local opt-ins for endpoint hygiene.
var switchKeys = []string{"allow_app_removal", "allow_startup_control", "enforce_actions"}

// rejectLookalikeKeys refuses a config in which a key is a case-folded spelling of
// an opt-in without being exactly it. encoding/json matches field names with
// Unicode folding, so "ALLOW_APP_REMOVAL" or "allow_ſtartup_control" (long s)
// would otherwise switch the capability on while passing any filter that looks for
// the canonical name. An opt-in has to be spelled exactly.
func rejectLookalikeKeys(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil // the typed decode below reports the syntax error
	}
	for k := range raw {
		for _, want := range switchKeys {
			if k != want && strings.EqualFold(k, want) {
				return fmt.Errorf("config: key %q must be spelled exactly %q", k, want)
			}
		}
	}
	return nil
}

// applyDefaults backfills any interval/limit left at zero after unmarshalling a
// partial config.json (GPO configs often carry only backend_url + token + CA).
func (c *Config) applyDefaults() {
	d := Default()
	if c.Intervals.VitalsSec <= 0 {
		c.Intervals.VitalsSec = d.Intervals.VitalsSec
	}
	if c.Intervals.SampleSec <= 0 {
		c.Intervals.SampleSec = d.Intervals.SampleSec
	}
	if c.Intervals.PostSec <= 0 {
		c.Intervals.PostSec = d.Intervals.PostSec
	}
	if c.Intervals.SmartMin <= 0 {
		c.Intervals.SmartMin = d.Intervals.SmartMin
	}
	if c.TopN <= 0 {
		c.TopN = d.TopN
	}
	if c.SpoolMaxMB <= 0 {
		c.SpoolMaxMB = d.SpoolMaxMB
	}
	if c.IdleThresholdSec <= 0 {
		c.IdleThresholdSec = d.IdleThresholdSec
	} else if c.IdleThresholdSec < 60 {
		c.IdleThresholdSec = 60 // floor: anything shorter is noise, not idleness
	}
	if c.LogLevel == "" {
		c.LogLevel = d.LogLevel
	}
	if c.BackendURL == "" {
		c.BackendURL = d.BackendURL
	}
	if c.HygieneHours < 1 { // floor 1h; 0/missing means "use the default"
		c.HygieneHours = d.HygieneHours
	}
	// NOTE: allow_app_removal / allow_startup_control / enforce_actions are
	// deliberately absent here. They are booleans whose zero value means "no";
	// backfilling them would turn a partial config.json into consent.
}

// Save writes config.json to ConfigPath (used by tests / manual provisioning; GPO
// normally provisions the file directly).
func Save(c *Config) error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath()), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), b, 0o600)
}

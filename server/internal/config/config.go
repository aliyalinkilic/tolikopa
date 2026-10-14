// Package config loads all runtime configuration from environment variables.
// No secrets are ever hard-coded; every value comes from the process env
// (injected by docker-compose from the gitignored .env file).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every tunable the server needs. Populated once at startup.
type Config struct {
	DatabaseURL string
	ListenAddr  string

	IngestToken string
	JWTSecret   string

	// Optional. Fresh installs leave these unset and create the first admin in
	// the browser (first-run setup); existing installs keep seeding from them.
	AdminUsername string
	AdminPassword string

	// SetupCode optionally fixes the one-time first-run setup code; when empty a
	// code is generated at startup and printed to the log.
	SetupCode string

	// ArtifactsDir holds the agent binaries, the setup tool and artifacts.json
	// (provided by the Docker image). May not exist in local development.
	ArtifactsDir string
	// TLSCertFile is the panel's TLS certificate (created by setup.sh), read on
	// demand. Agents pin it, and tolikopa-setup.exe makes domain PCs trust it.
	TLSCertFile string

	ServerHost string

	OfflineWarnSec int
	OfflineCritSec int

	// SMTP (optional e-mail notifications; disabled unless SMTPEnabled).
	SMTPEnabled  bool
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPass     string
	SMTPFrom     string
	AlertEmailTo string
}

// Load reads configuration from the environment, applying defaults and
// validating that the required secrets are present.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:    getenv("DATABASE_URL", ""),
		ListenAddr:     getenv("LISTEN_ADDR", ":8080"),
		IngestToken:    getenv("INGEST_TOKEN", ""),
		JWTSecret:      getenv("JWT_SECRET", ""),
		AdminUsername:  getenv("ADMIN_USERNAME", "admin"),
		AdminPassword:  getenv("ADMIN_PASSWORD", ""),
		SetupCode:      getenv("SETUP_CODE", ""),
		ArtifactsDir:   getenv("ARTIFACTS_DIR", "/opt/tolikopa/artifacts"),
		TLSCertFile:    getenv("TLS_CERT_FILE", "/tls/tolikopa.crt"),
		ServerHost:     getenv("SERVER_HOST", "localhost"),
		OfflineWarnSec: getenvInt("OFFLINE_WARN_SEC", 300),
		OfflineCritSec: getenvInt("OFFLINE_CRIT_SEC", 1800),
		SMTPEnabled:    getenvBool("SMTP_ENABLED", false),
		SMTPHost:       getenv("SMTP_HOST", ""),
		SMTPPort:       getenvInt("SMTP_PORT", 587),
		SMTPUser:       getenv("SMTP_USER", ""),
		SMTPPass:       getenv("SMTP_PASS", ""),
		SMTPFrom:       getenv("SMTP_FROM", ""),
		AlertEmailTo:   getenv("ALERT_EMAIL_TO", ""),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.IngestToken == "" {
		return nil, fmt.Errorf("INGEST_TOKEN is required")
	}
	if c.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	return c, nil
}

// OfflineWarn returns the offline-warning threshold as a duration.
func (c *Config) OfflineWarn() time.Duration {
	return time.Duration(c.OfflineWarnSec) * time.Second
}

// OfflineCrit returns the offline-critical threshold as a duration.
func (c *Config) OfflineCrit() time.Duration {
	return time.Duration(c.OfflineCritSec) * time.Second
}

// OriginPatterns returns the allowed WebSocket Origin patterns derived from the
// configured host. Same-origin behind Caddy, so the panel host is allowed with
// and without an explicit port, plus loopback for local development.
func (c *Config) OriginPatterns() []string {
	pats := []string{}
	// ServerHost may be a space-separated list (e.g. "tolikopa.local 10.0.0.10").
	for _, host := range strings.Fields(c.ServerHost) {
		pats = append(pats, host, host+":*")
	}
	pats = append(pats, "localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*")
	return pats
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

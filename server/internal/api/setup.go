package api

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/artifacts"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/bundle"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// The setup wizard backend and the rollout-tool downloads.
//
// The server never holds domain credentials — there is no field, table or
// endpoint for them, by decision. Rollout is done by tolikopa-setup.exe, which
// an administrator downloads here and runs on their own domain PC under their
// own session. The server's part is to make that one generic executable
// installation-specific by appending a bundle (internal/bundle).
//
// The ingest token leaves the server in exactly one place: inside config.json.
// It is never logged, never put in a manifest and only ever shown masked.

const (
	settingSetupConfig    = "setup_config"       // jsonb object: model.SetupConfig
	settingSetupCompleted = "setup_completed_at" // jsonb string (RFC 3339) or null

	defaultGPOName = "Tolikopa Agent"

	// Download names (the :name path parameter). A closed set.
	dlSetupTool  = "tolikopa-setup.exe"
	dlUpdateTool = "tolikopa-update.exe"
	dlAgentWin   = "tolikopa-agent.exe"
	dlAgentLinux = "tolikopa-agent-linux"
	dlBootstrap  = "bootstrap.ps1"
	dlConfig     = "config.json"

	maxCAFileSize = 64 << 10
)

// loadSetup reads the wizard config (defaults applied) and the completion time.
func (a *API) loadSetup(ctx context.Context) (model.SetupConfig, *time.Time, error) {
	cfg := model.SetupConfig{GPOName: defaultGPOName}
	var completed *time.Time
	rows, err := a.pool.Query(ctx,
		`SELECT key, value FROM app_setting WHERE key = ANY($1)`,
		[]string{settingSetupConfig, settingSetupCompleted})
	if err != nil {
		return cfg, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return cfg, nil, err
		}
		switch key {
		case settingSetupConfig:
			// Unknown or malformed content leaves the defaults in place.
			_ = json.Unmarshal(raw, &cfg)
			if cfg.GPOName == "" {
				cfg.GPOName = defaultGPOName
			}
		case settingSetupCompleted:
			var s *string
			if json.Unmarshal(raw, &s) == nil && s != nil {
				if t, err := time.Parse(time.RFC3339, *s); err == nil {
					completed = &t
				}
			}
		}
	}
	return cfg, completed, rows.Err()
}

// validateSetupConfig checks a complete config and normalises server_url. The
// rules for the domain fields are the setup tool's own (internal/bundle), so the
// panel refuses here what the tool would refuse on the administrator's PC.
// Empty server_url / domain_fqdn are allowed: they mean "not configured yet".
func validateSetupConfig(c *model.SetupConfig) error {
	bad := func(field, msg string) error {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest, field+": "+msg,
			map[string]any{"field": field})
	}
	c.ServerURL = strings.TrimSpace(c.ServerURL)
	if c.ServerURL != "" {
		norm, err := bundle.NormalizeServerURL(c.ServerURL)
		if err != nil {
			return bad("server_url", err.Error())
		}
		c.ServerURL = norm
	}
	if c.DomainFQDN != "" && !bundle.ValidFQDN(c.DomainFQDN) {
		return bad("domain_fqdn", "must be a DNS domain name such as corp.example.com")
	}
	if c.NetBIOS != "" && !bundle.ValidNetBIOS(c.NetBIOS) {
		return bad("netbios", "must be 1-15 letters, digits or hyphens")
	}
	if !bundle.ValidGPOName(c.GPOName) {
		return bad("gpo_name", "must be 1-63 letters, digits, spaces, _ or -, starting with a letter or digit")
	}
	if c.TargetDN != "" {
		if c.DomainFQDN == "" {
			return bad("target_dn", "set domain_fqdn first")
		}
		if !bundle.ValidTargetDN(c.TargetDN, c.DomainFQDN) {
			return bad("target_dn", "must be a distinguished name (OU=…,DC=…) inside the domain")
		}
	}
	return nil
}

// rootCA reads the panel's TLS certificate — the one agents pin in tls_ca_pem
// and tolikopa-setup.exe distributes as trusted. It is read on demand, so a
// certificate replaced on the host is picked up without a restart. The returned
// text is re-encoded from the parsed certificates, so nothing but CERTIFICATE
// blocks can pass through (never a private key).
func (a *API) rootCA() (string, error) {
	if a.cfg.TLSCertFile == "" {
		return "", errors.New("no TLS certificate file configured")
	}
	f, err := os.Open(a.cfg.TLSCertFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCAFileSize))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return "", err
		}
		out.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}))
	}
	if out.Len() == 0 {
		return "", errors.New("TLS certificate file holds no certificate")
	}
	return out.String(), nil
}

// readiness evaluates the four preconditions of a rollout. None of them depends
// on any machine having reported.
func (a *API) readiness(cfg model.SetupConfig) (model.SetupReadiness, *artifacts.Info) {
	_, caErr := a.rootCA()
	info, artErr := a.arts.Info()
	if artErr != nil {
		info = nil
	}
	return model.SetupReadiness{
		ServerURL: cfg.ServerURL != "",
		RootCA:    caErr == nil,
		Artifacts: info != nil,
		Domain:    cfg.DomainFQDN != "",
	}, info
}

func missingOf(r model.SetupReadiness, need ...string) []string {
	have := map[string]bool{"server_url": r.ServerURL, "root_ca": r.RootCA, "artifacts": r.Artifacts, "domain": r.Domain}
	missing := []string{}
	for _, n := range need {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	return missing
}

func setupIncomplete(missing []string) error {
	return apierr.WithParams(http.StatusConflict, apierr.SetupIncomplete,
		"setup is incomplete: "+strings.Join(missing, ", "), map[string]any{"missing": missing})
}

func artifactsUnavailable() error {
	return apierr.New(http.StatusServiceUnavailable, apierr.ArtifactsUnavailable,
		"build artifacts are not available on this server")
}

// normVersion makes "v1.8.2" and "1.8.2" compare equal.
func normVersion(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "v")
}

// serverURLCandidates suggests values for server_url: every SERVER_HOST entry
// plus the origin this very request arrived on (which demonstrably works).
func (a *API) serverURLCandidates(c echo.Context) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(host string) {
		u, err := bundle.NormalizeServerURL("https://" + host)
		if err != nil || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, h := range strings.Fields(a.cfg.ServerHost) {
		add(h)
	}
	add(c.Request().Host)
	return out
}

// SetupStatus handles GET /api/setup/status (no auth).
func (a *API) SetupStatus(c echo.Context) error {
	ctx := c.Request().Context()
	needs, err := a.auth.NeedsAdmin(ctx)
	if err != nil {
		return serverError(err)
	}
	_, completed, err := a.loadSetup(ctx)
	if err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, model.SetupStatus{NeedsAdmin: needs, SetupComplete: completed != nil})
}

// GetSetup handles GET /api/admin/setup: everything the wizard and the later
// "Setup" page show, in one object.
func (a *API) GetSetup(c echo.Context) error {
	ctx := c.Request().Context()
	cfg, completed, err := a.loadSetup(ctx)
	if err != nil {
		return serverError(err)
	}
	ready, info := a.readiness(cfg)

	out := model.SetupOverview{
		Config:              cfg,
		CompletedAt:         completed,
		IngestTokenMasked:   maskToken(a.cfg.IngestToken),
		Readiness:           ready,
		Artifacts:           info,
		ServerURLCandidates: a.serverURLCandidates(c),
	}
	out.Fleet.ByAgentVersion = []model.AgentVersionCount{}

	// Windows machines only: tolikopa-update.exe and the NETLOGON payload update
	// the Windows agent, so a Linux host (the server monitoring itself, updated
	// separately) can neither be brought "current" by it nor should it raise the
	// update banner.
	rows, err := a.pool.Query(ctx, `
		SELECT COALESCE(agent_version, ''), count(*)::int
		FROM machine WHERE archived_at IS NULL AND kind <> 'linux'
		GROUP BY 1 ORDER BY 2 DESC, 1`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var vc model.AgentVersionCount
		if err := rows.Scan(&vc.Version, &vc.Count); err != nil {
			return serverError(err)
		}
		out.Fleet.ByAgentVersion = append(out.Fleet.ByAgentVersion, vc)
		out.Fleet.Total += vc.Count
		if info != nil && normVersion(vc.Version) == normVersion(info.AgentVersion) {
			out.Fleet.Current += vc.Count
		}
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	if info != nil {
		out.Fleet.Outdated = out.Fleet.Total - out.Fleet.Current
		out.UpdateAvailable = out.Fleet.Outdated > 0
	}
	noStoreHeader(c)
	return c.JSON(http.StatusOK, out)
}

// PutSetupConfig handles PUT /api/admin/setup/config. Partial: an omitted field
// keeps its value. Returns the stored config.
func (a *API) PutSetupConfig(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		ServerURL         *string `json:"server_url"`
		DomainFQDN        *string `json:"domain_fqdn"`
		NetBIOS           *string `json:"netbios"`
		TargetDN          *string `json:"target_dn"`
		GPOName           *string `json:"gpo_name"`
		DefenderExclusion *bool   `json:"defender_exclusion"`
	}
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	// A body naming none of the fields (e.g. wrapped in another object) would
	// otherwise "succeed" while changing nothing.
	if body.ServerURL == nil && body.DomainFQDN == nil && body.NetBIOS == nil &&
		body.TargetDN == nil && body.GPOName == nil && body.DefenderExclusion == nil {
		return apierr.BadRequest("no config field in request body")
	}
	cfg, _, err := a.loadSetup(ctx)
	if err != nil {
		return serverError(err)
	}
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&cfg.ServerURL, body.ServerURL)
	set(&cfg.DomainFQDN, body.DomainFQDN)
	set(&cfg.NetBIOS, body.NetBIOS)
	set(&cfg.TargetDN, body.TargetDN)
	set(&cfg.GPOName, body.GPOName)
	if body.DefenderExclusion != nil {
		cfg.DefenderExclusion = *body.DefenderExclusion
	}
	if err := validateSetupConfig(&cfg); err != nil {
		return err
	}
	raw, _ := json.Marshal(cfg)
	if _, err := a.pool.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		settingSetupConfig, raw); err != nil {
		return serverError(err)
	}
	actor, _ := actorOf(c)
	log.Printf("setup: %q updated the rollout config (server_url=%q domain=%q)", actor, cfg.ServerURL, cfg.DomainFQDN)
	return c.JSON(http.StatusOK, cfg)
}

// CompleteSetup handles POST /api/admin/setup/complete. All four readiness
// checks must hold; no machine needs to have reported yet.
func (a *API) CompleteSetup(c echo.Context) error {
	ctx := c.Request().Context()
	cfg, _, err := a.loadSetup(ctx)
	if err != nil {
		return serverError(err)
	}
	ready, _ := a.readiness(cfg)
	if missing := missingOf(ready, "server_url", "root_ca", "artifacts", "domain"); len(missing) > 0 {
		return setupIncomplete(missing)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := a.setCompleted(ctx, &now); err != nil {
		return serverError(err)
	}
	actor, _ := actorOf(c)
	log.Printf("setup: %q marked setup complete", actor)
	return c.JSON(http.StatusOK, model.SetupCompletion{CompletedAt: &now})
}

// ReopenSetup handles POST /api/admin/setup/reopen.
func (a *API) ReopenSetup(c echo.Context) error {
	if err := a.setCompleted(c.Request().Context(), nil); err != nil {
		return serverError(err)
	}
	actor, _ := actorOf(c)
	log.Printf("setup: %q reopened setup", actor)
	return c.JSON(http.StatusOK, model.SetupCompletion{})
}

func (a *API) setCompleted(ctx context.Context, at *time.Time) error {
	raw := []byte("null")
	if at != nil {
		raw, _ = json.Marshal(at.Format(time.RFC3339))
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO app_setting (key, value, updated_at) VALUES ($1, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		settingSetupCompleted, raw)
	return err
}

func noStoreHeader(c echo.Context) {
	c.Response().Header().Set("Cache-Control", "no-store")
}

// DownloadSetupFile handles GET /api/admin/setup/download/{name}.
func (a *API) DownloadSetupFile(c echo.Context) error {
	ctx := c.Request().Context()
	name := c.Param("name")

	var (
		data []byte
		ct   = "application/octet-stream"
		err  error
	)
	switch name {
	case dlAgentWin:
		data, _, err = a.arts.Read(artifacts.AgentWindows)
	case dlAgentLinux:
		data, _, err = a.arts.Read(artifacts.AgentLinux)
	case dlBootstrap:
		data, _, err = a.arts.Read(artifacts.Bootstrap)
		ct = "text/plain; charset=utf-8"
	case dlConfig, dlSetupTool, dlUpdateTool:
		cfg, _, lerr := a.loadSetup(ctx)
		if lerr != nil {
			return serverError(lerr)
		}
		if name == dlConfig {
			data, err = a.buildAgentConfig(cfg)
			ct = "application/json"
		} else {
			lang := c.QueryParam("lang")
			if !auth.ValidLang(lang) {
				g, gerr := a.loadGeneral(ctx)
				if gerr != nil {
					return serverError(gerr)
				}
				lang = g.DefaultLang
			}
			mode := bundle.ModeSetup
			if name == dlUpdateTool {
				mode = bundle.ModeUpdate
			}
			data, err = a.buildTool(cfg, mode, lang)
		}
	default:
		return apierr.NotFoundf("unknown download")
	}
	if errors.Is(err, artifacts.ErrUnavailable) {
		return artifactsUnavailable()
	}
	if err != nil {
		var he *echo.HTTPError
		if errors.As(err, &he) {
			return err
		}
		return serverError(err)
	}

	// Every download is recorded: these files carry the ingest token and are
	// what gets pushed to the whole domain. RealIP is the hardened extractor.
	actor, _ := actorOf(c)
	log.Printf("setup: download %s by %q from %s (%d bytes)", name, actor, c.RealIP(), len(data))

	h := c.Response().Header()
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, ct, data)
}

// buildAgentConfig renders config.json. Needs server_url and the root CA.
func (a *API) buildAgentConfig(cfg model.SetupConfig) ([]byte, error) {
	ca, caErr := a.rootCA()
	missing := []string{}
	if cfg.ServerURL == "" {
		missing = append(missing, "server_url")
	}
	if caErr != nil {
		missing = append(missing, "root_ca")
	}
	if len(missing) > 0 {
		return nil, setupIncomplete(missing)
	}
	return bundle.NewAgentConfig(cfg.ServerURL, a.cfg.IngestToken, ca).JSON()
}

// buildTool assembles tolikopa-setup.exe / tolikopa-update.exe: the generic
// executable plus the bundle for this installation.
func (a *API) buildTool(cfg model.SetupConfig, mode, lang string) ([]byte, error) {
	// Artifacts first: without them this is a 503, whatever else is missing.
	meta, err := a.arts.Meta()
	if err != nil {
		return nil, err
	}
	exe, _, err := a.arts.Read(artifacts.SetupTool)
	if err != nil {
		return nil, err
	}
	agent, _, err := a.arts.Read(artifacts.AgentWindows)
	if err != nil {
		return nil, err
	}
	bootstrap, _, err := a.arts.Read(artifacts.Bootstrap)
	if err != nil {
		return nil, err
	}

	missing := []string{}
	if cfg.ServerURL == "" {
		missing = append(missing, "server_url")
	}
	ca, caErr := a.rootCA()
	if caErr != nil {
		missing = append(missing, "root_ca")
	}
	if cfg.DomainFQDN == "" {
		missing = append(missing, "domain")
	}
	if len(missing) > 0 {
		return nil, setupIncomplete(missing)
	}
	conf, err := bundle.NewAgentConfig(cfg.ServerURL, a.cfg.IngestToken, ca).JSON()
	if err != nil {
		return nil, err
	}

	m := bundle.Manifest{
		Mode:           mode,
		Lang:           lang,
		ProductVersion: meta.ProductVersion,
		AgentVersion:   meta.AgentVersion,
		ServerURL:      cfg.ServerURL,
		DomainFQDN:     cfg.DomainFQDN,
		NetBIOS:        cfg.NetBIOS,
	}
	if mode == bundle.ModeSetup {
		// The GPO fields exist only for a setup run; an update never touches the GPO.
		m.TargetDN = cfg.TargetDN
		m.GPOName = cfg.GPOName
		m.DefenderExclusion = cfg.DefenderExclusion
	}
	out, err := bundle.Assemble(exe, m, []bundle.Content{
		{Name: bundle.FileAgent, Data: agent},
		{Name: bundle.FileBootstrap, Data: bootstrap},
		{Name: bundle.FileConfig, Data: conf},
	})
	var fe *bundle.FieldError
	if errors.As(err, &fe) {
		// The stored config no longer satisfies the tool's rules (e.g. edited
		// outside the API). Point the admin at the field instead of a 500.
		return nil, setupIncomplete([]string{fe.Field})
	}
	return out, err
}

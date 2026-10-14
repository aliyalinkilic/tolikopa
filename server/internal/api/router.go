package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
)

// Router builds the Echo instance with middleware and all routes mounted.
func (a *API) Router() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	// One error shape for everything: {"code","message"[,"params"]} (docs/API.md).
	e.HTTPErrorHandler = apierr.Handler

	// Echo's default RealIP() trusts the FIRST X-Forwarded-For element, and Caddy
	// appends to that header rather than replacing it — so a client could dictate
	// the IP recorded in remediation_policy_audit, the permanent record of who
	// armed a fleet-wide destructive capability. This extractor walks the chain
	// from the right and stops at the first address outside our trusted proxy
	// range, which is the reverse proxy's own view of the client.
	e.IPExtractor = echo.ExtractIPFromXFFHeader(
		echo.TrustLoopback(true),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(true),
	)

	// Global middleware.
	e.Use(middleware.Recover())
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `${time_rfc3339} ${method} ${uri} ${status} ${latency_human}` + "\n",
	}))

	// WebSocket lives at the root (Caddy proxies /ws). It must NOT be wrapped by
	// the gzip middleware or the connection cannot be hijacked.
	e.GET("/ws", a.hub.Handle)

	// Everything else under /api gets gzip + a body limit.
	api := e.Group("/api")
	api.Use(middleware.Gzip())
	api.Use(middleware.BodyLimit("4M"))

	api.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Agent ingest (bearer token).
	api.POST("/v1/ingest", a.ingester.Handle, a.ingester.RequireIngest)

	// Unauthenticated instance info for the login page (org name, default
	// language, version). Nothing sensitive may ever be added to it.
	api.GET("/settings/public", a.PublicSettings)

	// First run (no auth). Creating the first admin is guarded by the one-time
	// setup code and only works while admin_user is empty.
	api.GET("/setup/status", a.SetupStatus)
	api.POST("/setup/admin", a.auth.SetupAdmin)

	// Auth. /auth/* stays reachable during forced 2FA enrolment.
	api.POST("/auth/login", a.auth.Login)
	api.POST("/auth/login/mfa", a.auth.LoginMFA)
	api.POST("/auth/logout", a.auth.Logout, a.auth.RequireAuth)
	api.GET("/auth/me", a.auth.Me, a.auth.RequireAuth)

	// Self-service for any signed-in user (viewer or admin). Writes are CSRF
	// protected by RequireAuth like every other write.
	me := api.Group("/me", a.auth.RequireAuth)
	me.PUT("/profile", a.auth.PutProfile)
	me.PUT("/prefs", a.auth.PutPrefs)
	me.PUT("/password", a.auth.PutPassword)
	me.POST("/2fa/setup", a.auth.TOTPSetup)
	me.POST("/2fa/enable", a.auth.TOTPEnable)
	me.POST("/2fa/disable", a.auth.TOTPDisable)
	me.POST("/2fa/recovery-codes", a.auth.TOTPRecoveryCodes)

	// Authenticated (viewer or admin).
	sec := api.Group("", a.auth.RequireAuth)
	sec.GET("/fleet", a.Fleet)
	sec.GET("/fleet/summary", a.FleetSummary)
	sec.GET("/fleet/overview", a.FleetOverview)
	sec.GET("/stats/hourly-activity", a.StatsHourlyActivity)
	sec.GET("/stats/top-apps", a.StatsTopApps)
	sec.GET("/stats/slow-machines", a.SlowMachines)
	// /api/v1 aliases (the dashboard contract names them under v1; same handlers,
	// same cookie auth — /api/v1 otherwise carries only the agent ingest route).
	sec.GET("/v1/fleet/overview", a.FleetOverview)
	sec.GET("/v1/stats/hourly-activity", a.StatsHourlyActivity)
	sec.GET("/v1/stats/top-apps", a.StatsTopApps)
	sec.GET("/v1/stats/slow-machines", a.SlowMachines)

	sec.GET("/machines/:id", a.MachineDetail)
	sec.GET("/machines/:id/metrics", a.MachineMetrics)
	sec.GET("/machines/:id/disks", a.MachineDisks)
	sec.GET("/machines/:id/top-processes", a.MachineTopProcesses)
	sec.GET("/machines/:id/categories", a.MachineCategories)
	sec.GET("/machines/:id/sessions", a.MachineSessions)
	sec.GET("/machines/:id/events", a.MachineEvents)
	sec.GET("/machines/:id/alerts", a.MachineAlerts)
	// Endpoint hygiene, read-only per machine (viewer scope). Presence of
	// ALLOWLISTED catalog items only — never a software inventory.
	sec.GET("/machines/:id/remediation", a.MachineRemediation)

	sec.GET("/users", a.Users)
	sec.GET("/users/:user/timeline", a.UserTimeline)
	sec.GET("/users/:user/logons", a.UserLogons)
	sec.GET("/users/:user/network", a.UserNetwork)
	sec.GET("/users/:user/categories", a.UserCategories)
	sec.GET("/users/:user/activity", a.UserActivity)
	sec.GET("/users/:user/apps", a.UserApps)

	sec.GET("/tags/:tag/summary", a.TagSummary)

	sec.GET("/alerts", a.Alerts)
	sec.POST("/alerts/:id/ack", a.AckAlert)

	// Admin-only.
	adm := api.Group("/admin", a.auth.RequireAdmin)
	adm.PUT("/machines/:id/labels", a.PutMachineLabels)
	adm.POST("/machines/archive", a.ArchiveMachines)
	adm.POST("/machines/restore", a.RestoreMachines)
	adm.GET("/machines/archived", a.ArchivedMachines)
	adm.GET("/categories", a.ListCategories)
	adm.POST("/categories", a.CreateCategory)
	adm.PUT("/categories/:id", a.UpdateCategory)
	adm.DELETE("/categories/:id", a.DeleteCategory)
	adm.GET("/unclassified", a.Unclassified)
	adm.POST("/reclassify", a.Reclassify)
	adm.GET("/alert-rules", a.GetAlertRules)
	adm.PUT("/alert-rules", a.PutAlertRules)
	// Panel accounts (who may sign in here).
	adm.GET("/panel-users", a.ListPanelUsers)
	adm.POST("/panel-users", a.CreatePanelUser)
	adm.PATCH("/panel-users/:username", a.PatchPanelUser)
	adm.POST("/panel-users/:username/reset-password", a.ResetPanelUserPassword)
	adm.POST("/panel-users/:username/reset-2fa", a.ResetPanelUser2FA)
	adm.DELETE("/panel-users/:username", a.DeletePanelUser)
	// Legacy aliases of list/create. NOT the same thing as PUT /users/:user below,
	// which edits a monitored DOMAIN user's profile.
	adm.GET("/users", a.ListPanelUsers)
	adm.POST("/users", a.CreatePanelUser)
	adm.PUT("/users/:user", a.AdminSetUserProfile)
	// Setup wizard + rollout-tool downloads.
	adm.GET("/setup", a.GetSetup)
	adm.PUT("/setup/config", a.PutSetupConfig)
	adm.POST("/setup/complete", a.CompleteSetup)
	adm.POST("/setup/reopen", a.ReopenSetup)
	adm.GET("/setup/download/:name", a.DownloadSetupFile)
	adm.GET("/settings/general", a.GetGeneralSettings)
	adm.PUT("/settings/general", a.PutGeneralSettings)
	adm.GET("/tags", a.AdminTags)
	adm.GET("/ingest-token", a.IngestTokenInfo)
	// Endpoint hygiene. Flat paths on purpose: the /admin/categories routes and the
	// frontend's calls to them already disagree (PUT/DELETE /admin/categories vs
	// /admin/categories/:id), so nothing here copies that shape.
	adm.GET("/remediation", a.GetRemediation)
	adm.PUT("/remediation/policy", a.PutRemediationPolicy)
	adm.GET("/remediation/attempts", a.GetRemediationAttempts)
	adm.GET("/remediation/failures", a.GetRemediationFailures)
	adm.GET("/remediation/impact", a.GetRemediationImpact)

	return e
}

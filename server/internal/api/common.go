// Package api implements the REST + WebSocket HTTP surface (docs/API.md §2-§8).
// All queries use parameterized pgx; live reads come from machine_live, history
// from the continuous aggregates.
package api

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/artifacts"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/ingest"
	"github.com/aliyalinkilic/tolikopa/server/internal/ws"
)

// API bundles the dependencies shared by every handler.
type API struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	cats     *ingest.Categories
	pols     *ingest.Policies
	auth     *auth.Auth
	ingester *ingest.Ingester
	hub      *ws.Hub
	arts     *artifacts.Store
}

// New builds the API handler set. pols is the remediation-policy snapshot the
// ingest path resolves against; the admin write path Refresh()es it after a
// policy change, exactly as it does with cats after a classification change.
func New(pool *pgxpool.Pool, cfg *config.Config, cats *ingest.Categories, pols *ingest.Policies, a *auth.Auth, ig *ingest.Ingester, hub *ws.Hub) *API {
	return &API{
		pool: pool, cfg: cfg, cats: cats, pols: pols, auth: a, ingester: ig, hub: hub,
		arts: artifacts.New(cfg.ArtifactsDir),
	}
}

// parseRange reads from/to query params (RFC3339). Defaults: to=now, from=now-6h.
func parseRange(c echo.Context) (time.Time, time.Time) {
	to := time.Now()
	if v := c.QueryParam("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	from := to.Add(-6 * time.Hour)
	if v := c.QueryParam("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	return from, to
}

// pickResolution honors an explicit res param, otherwise selects a source by the
// range width: <=6h raw, <=7d 5m, else 1h.
func pickResolution(res string, from, to time.Time) string {
	switch res {
	case "raw", "5m", "1h":
		return res
	}
	span := to.Sub(from)
	switch {
	case span <= 6*time.Hour:
		return "raw"
	case span <= 7*24*time.Hour:
		return "5m"
	default:
		return "1h"
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func queryInt(c echo.Context, name string, def int) int {
	if v := c.QueryParam(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// jsonbToMap decodes a jsonb column ([]byte) into a map.
func jsonbToMap(b []byte) map[string]any {
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// serverError logs the real error and returns a generic message — raw driver/SQL
// error text (table names, constraints) must never reach the client.
func serverError(err error) error {
	return apierr.Internal("api", err)
}

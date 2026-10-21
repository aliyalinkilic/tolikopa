package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/alerts"
	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
	"github.com/aliyalinkilic/tolikopa/server/internal/workhours"
)

const alertSelect = `
	SELECT a.id, a.machine_id, m.hostname, a.rule, a.severity, a.status, a.value, a.message,
	       a.opened_at, a.last_seen_at, a.resolved_at, a.detail, a.acked_at, m.tags
	FROM alert a LEFT JOIN machine m ON m.machine_id = a.machine_id`

// Alerts handles GET /api/alerts.
func (a *API) Alerts(c echo.Context) error {
	ctx := c.Request().Context()
	// Alerts of archived machines are hidden with the machine (0013).
	sql := alertSelect + " WHERE (a.machine_id IS NULL OR m.archived_at IS NULL)"
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		sql += " AND " + cond + "$" + strconv.Itoa(len(args))
	}
	if v := c.QueryParam("status"); v != "" {
		add("a.status=", v)
	}
	if v := c.QueryParam("severity"); v != "" {
		add("a.severity=", v)
	}
	if v := c.QueryParam("machine"); v != "" {
		add("a.machine_id=", v)
	}
	if v := c.QueryParam("rule"); v != "" {
		add("a.rule=", v)
	}
	if v := c.QueryParam("tag"); v != "" {
		// Machine tag, case-insensitive (like the work-hours tag match).
		args = append(args, v)
		sql += " AND EXISTS (SELECT 1 FROM unnest(m.tags) t WHERE lower(t) = lower($" + strconv.Itoa(len(args)) + "))"
	}
	if v := c.QueryParam("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("a.opened_at>=", t)
		}
	}
	if v := c.QueryParam("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("a.opened_at<=", t)
		}
	}
	sql += " ORDER BY a.opened_at DESC LIMIT 1000"

	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out, err := scanAlerts(rows)
	if err != nil {
		return serverError(err)
	}
	// Flag alerts of work-hours-only machines so the page can say why an
	// offline alert closed at the end of the day. A failed read uses the default.
	w, _ := workhours.Load(ctx, a.pool)
	sched := workhours.Compile(w)
	for i := range out {
		out[i].WorkHours = sched.Applies(out[i].MachineTags)
	}
	return c.JSON(http.StatusOK, out)
}

// AckAlert handles POST /api/alerts/{id}/ack.
func (a *API) AckAlert(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	var status string
	err = a.pool.QueryRow(ctx, `
		UPDATE alert SET status='ack', acked_at=now()
		WHERE id=$1 AND status='open' RETURNING status`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFoundf("no open alert with this id")
	}
	if err != nil {
		return serverError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func scanAlerts(rows pgx.Rows) ([]model.AlertDTO, error) {
	out := []model.AlertDTO{}
	for rows.Next() {
		var d model.AlertDTO
		var host *string
		var mid *string
		var val *float64
		var msg *string
		var resolved *time.Time
		var detail []byte
		var tags []string
		if err := rows.Scan(&d.ID, &mid, &host, &d.Rule, &d.Severity, &d.Status, &val, &msg,
			&d.OpenedAt, &d.LastSeenAt, &resolved, &detail, &d.AckedAt, &tags); err != nil {
			return nil, err
		}
		d.MachineTags = tags
		if d.MachineTags == nil {
			d.MachineTags = []string{}
		}
		if mid != nil {
			d.MachineID = *mid
		}
		if host != nil {
			d.Hostname = *host
		}
		d.Value = val
		if msg != nil {
			d.Message = *msg
		}
		d.ResolvedAt = resolved
		d.Detail = jsonbToMap(detail)
		// Params is what clients localise from; CompleteParams also back-fills the
		// derivable values on rows written before alerts carried them.
		d.Params = alerts.CompleteParams(d.Rule, d.Detail, d.Hostname, d.Value)
		out = append(out, d)
	}
	return out, rows.Err()
}

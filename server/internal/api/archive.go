package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Machine archive (retiring a PC).
//
// Archiving hides a retired PC from every current-state surface while keeping
// its history; see 0013_machine_archive.sql. It is reversible from the panel and
// reverses itself when the agent reports again (ingest auto_restore), so an
// accidental archive loses nothing.

const maxArchiveBatch = 500

// ArchiveMachines handles POST /api/admin/machines/archive {machine_ids:[…]}.
//
// Transactional per call: the machines are marked, their open alerts resolved
// (an archived PC must not keep a red "offline" alert on the board), their
// open usage sessions closed at the last sample, and one audit row written each.
func (a *API) ArchiveMachines(c echo.Context) error {
	return a.setArchived(c, true)
}

// RestoreMachines handles POST /api/admin/machines/restore {machine_ids:[…]}.
func (a *API) RestoreMachines(c echo.Context) error {
	return a.setArchived(c, false)
}

func (a *API) setArchived(c echo.Context, archive bool) error {
	ctx := c.Request().Context()

	var body model.MachineArchiveRequest
	if err := apierr.Bind(c, &body); err != nil {
		return err
	}
	ids := make([]string, 0, len(body.MachineIDs))
	seen := make(map[string]struct{}, len(body.MachineIDs))
	for _, raw := range body.MachineIDs {
		id := strings.TrimSpace(raw)
		if id == "" || len(id) > 128 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return apierr.BadRequest("no machines selected")
	}
	if len(ids) > maxArchiveBatch {
		return apierr.WithParams(http.StatusBadRequest, "too_many_items", "at most 500 machines per request", map[string]any{"max": 500})
	}

	actor := ""
	if claims, ok := auth.ClaimsFrom(c); ok && claims != nil {
		actor = claims.Subject
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(err)
	}
	defer tx.Rollback(ctx)

	// Only rows that actually change state are touched and audited, so repeating a
	// request (double click, stale tab) is a no-op rather than a second record.
	var changed []string
	if archive {
		rows, err := tx.Query(ctx, `
			UPDATE machine SET archived_at = now(), archived_by = $2
			WHERE machine_id = ANY($1) AND archived_at IS NULL
			RETURNING machine_id`, ids, nullStr(actor))
		if err != nil {
			return serverError(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return serverError(err)
			}
			changed = append(changed, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return serverError(err)
		}
		if len(changed) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE alert SET status = 'resolved', resolved_at = now()
				WHERE machine_id = ANY($1) AND status <> 'resolved'`, changed); err != nil {
				return serverError(err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE usage_session us
				SET ended_at = GREATEST(us.started_at, COALESCE(ml.ts, now())), is_open = false
				FROM machine m LEFT JOIN machine_live ml ON ml.machine_id = m.machine_id
				WHERE m.machine_id = us.machine_id AND us.machine_id = ANY($1) AND us.is_open`,
				changed); err != nil {
				return serverError(err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE machine_live SET open_alerts = 0 WHERE machine_id = ANY($1)`, changed); err != nil {
				return serverError(err)
			}
		}
	} else {
		rows, err := tx.Query(ctx, `
			UPDATE machine SET archived_at = NULL, archived_by = NULL
			WHERE machine_id = ANY($1) AND archived_at IS NOT NULL
			RETURNING machine_id`, ids)
		if err != nil {
			return serverError(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return serverError(err)
			}
			changed = append(changed, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return serverError(err)
		}
	}

	if len(changed) > 0 {
		action := "restore"
		if archive {
			action = "archive"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO machine_archive_audit (machine_id, hostname, action, actor, actor_ip)
			SELECT m.machine_id, m.hostname, $2, $3, $4 FROM machine m WHERE m.machine_id = ANY($1)`,
			changed, action, nullStr(actor), nullStr(c.RealIP())); err != nil {
			return serverError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, model.MachineArchiveResult{Changed: len(changed)})
}

// ArchivedMachines handles GET /api/admin/machines/archived.
func (a *API) ArchivedMachines(c echo.Context) error {
	ctx := c.Request().Context()
	rows, err := a.pool.Query(ctx, `
		SELECT m.machine_id, m.hostname, m.kind, m.os, m.agent_version, m.first_seen,
		       COALESCE(ml.ts, m.last_seen), m.archived_at, m.archived_by
		FROM machine m LEFT JOIN machine_live ml ON ml.machine_id = m.machine_id
		WHERE m.archived_at IS NOT NULL
		ORDER BY m.archived_at DESC`)
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()
	out := model.ArchivedMachinesResponse{Machines: []model.ArchivedMachine{}}
	for rows.Next() {
		var (
			m                   model.ArchivedMachine
			osName, ver, archBy *string
		)
		if err := rows.Scan(&m.MachineID, &m.Hostname, &m.Kind, &osName, &ver, &m.FirstSeen,
			&m.LastSeen, &m.ArchivedAt, &archBy); err != nil {
			return serverError(err)
		}
		m.OS, m.AgentVersion, m.ArchivedBy = sv(osName), sv(ver), sv(archBy)
		out.Machines = append(out.Machines, m)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	return c.JSON(http.StatusOK, out)
}

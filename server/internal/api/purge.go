package api

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Permanent machine deletion.
//
// Purge is the irreversible step after archive: only a machine that is already
// archived can be purged, so a live PC can never be wiped by one stray click.
// Everything keyed by the machine goes — inventory, live row, raw time series,
// sessions, alerts and hygiene posture — except the hygiene attempts
// (remediation_attempt, see purgeRetained). The machine_archive_audit 'purge'
// row records who deleted PC-042, when, from which IP, and how many attempts
// were kept, so the question stays answerable after the machine is gone.
//
// If the agent on that PC is still installed it simply reports again: ingest
// upserts a fresh machine row under the same machine_id, with no history.

// CodeMachineNotArchived is returned when a purge names a machine that is not
// archived. Nothing is deleted in that case.
const CodeMachineNotArchived = "machine_not_archived"

// maxPurgeBatch caps one purge request. It is far below maxArchiveBatch: each
// machine costs one DELETE per compressed hypertable inside a single
// transaction (possibly decompressing chunks), so a large batch would hold
// row locks and a connection for a long time.
const maxPurgeBatch = 50

// purgeCompressed are hypertables with compression enabled (segmentby
// machine_id). They are deleted one machine at a time so TimescaleDB can drop
// whole compressed batches instead of decompressing them.
var purgeCompressed = []string{
	"metric_sample", "disk_usage_sample", "process_sample", "app_focus_sample",
}

// purgePlain are the remaining tables keyed by machine_id. machine_live,
// disk_health and machine_remediation_state also cascade from machine; they are
// listed so the purge does not depend on the FK definitions.
var purgePlain = []string{
	"event", "usage_session", "alert",
	"machine_live", "disk_health", "machine_remediation_state",
}

// purgeRetained are tables keyed by machine_id that a purge deliberately keeps.
//
// remediation_attempt is the actuation audit trail (CLAUDE.md, invariant 5):
// what the agent removed or disabled on a PC must stay answerable after the PC
// is deleted from the panel. Its rows age out with the table's 365-day
// retention policy. Admin attempt views hide them or label the machine as
// deleted, with the hostname from the purge audit row (attemptHostSQL).
var purgeRetained = []string{"remediation_attempt"}

// purgeCaggSources maps each raw hypertable to the continuous aggregates built
// on it, in refresh order (metric_1h is hierarchical on metric_5m).
var purgeCaggSources = []struct {
	table string
	caggs []string
}{
	{"metric_sample", []string{"metric_5m", "metric_1h", "net_user_1h", "session_user_1h"}},
	{"process_sample", []string{"proc_cat_1h", "proc_cat_user_1h", "proc_name_1h"}},
	{"app_focus_sample", []string{"app_focus_user_1h"}},
}

// PurgeMachines handles POST /api/admin/machines/purge {machine_ids:[…]}.
func (a *API) PurgeMachines(c echo.Context) error {
	ctx := c.Request().Context()

	ids, err := bindMachineIDs(c)
	if err != nil {
		return err
	}
	if len(ids) > maxPurgeBatch {
		return apierr.WithParams(http.StatusBadRequest, "too_many_items",
			"at most 50 machines per purge request", map[string]any{"max": maxPurgeBatch})
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

	// Lock the rows: an agent reporting mid-purge waits on its machine upsert and
	// then re-registers cleanly after commit, and a concurrent restore cannot slip
	// a machine out of the archive between this check and the delete.
	rows, err := tx.Query(ctx, `
		SELECT machine_id, archived_at IS NOT NULL
		FROM machine WHERE machine_id = ANY($1)
		ORDER BY machine_id
		FOR UPDATE`, ids)
	if err != nil {
		return serverError(err)
	}
	var found, notArchived []string
	for rows.Next() {
		var (
			id       string
			archived bool
		)
		if err := rows.Scan(&id, &archived); err != nil {
			rows.Close()
			return serverError(err)
		}
		if archived {
			found = append(found, id)
		} else {
			notArchived = append(notArchived, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	if len(notArchived) > 0 {
		return apierr.WithParams(http.StatusConflict, CodeMachineNotArchived,
			"only archived machines can be permanently deleted",
			map[string]any{"machine_ids": notArchived, "count": len(notArchived)})
	}
	if len(found) == 0 {
		// Already gone (double click, stale tab): nothing to do.
		return c.JSON(http.StatusOK, model.MachinePurgeResult{Purged: 0})
	}

	// Oldest raw row per cagg source, read before the delete: the aggregates are
	// refreshed from there afterwards (see refreshAfterPurge).
	since := map[string]time.Time{}
	for _, src := range purgeCaggSources {
		var ts *time.Time
		if err := tx.QueryRow(ctx,
			"SELECT min(ts) FROM "+src.table+" WHERE machine_id = ANY($1)", found).Scan(&ts); err != nil {
			return serverError(err)
		}
		if ts != nil {
			since[src.table] = *ts
		}
	}

	// Audit first, while the hostname is still readable: who (panel username),
	// when (ts default now()), from where (client IP), which machine, and how
	// many hygiene attempts stay behind as audit trail.
	if _, err := tx.Exec(ctx, `
		INSERT INTO machine_archive_audit (machine_id, hostname, action, actor, actor_ip, retained_attempts)
		SELECT m.machine_id, m.hostname, 'purge', $2, $3,
		       (SELECT count(*) FROM remediation_attempt ra WHERE ra.machine_id = m.machine_id)
		FROM machine m WHERE m.machine_id = ANY($1)`,
		found, nullStr(actor), nullStr(c.RealIP())); err != nil {
		return serverError(err)
	}

	// Deleting from compressed chunks may decompress; lift the per-transaction
	// cap (default 100k tuples) for this transaction only.
	if _, err := tx.Exec(ctx,
		"SELECT set_config('timescaledb.max_tuples_decompressed_per_dml_transaction', '0', true)"); err != nil {
		return serverError(err)
	}
	for _, table := range purgeCompressed {
		for _, id := range found {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE machine_id = $1", id); err != nil {
				return serverError(err)
			}
		}
	}
	for _, table := range purgePlain {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE machine_id = ANY($1)", found); err != nil {
			return serverError(err)
		}
	}
	tag, err := tx.Exec(ctx, "DELETE FROM machine WHERE machine_id = ANY($1)", found)
	if err != nil {
		return serverError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(err)
	}

	purgeRefreshes.schedule(a.pool, since)

	return c.JSON(http.StatusOK, model.MachinePurgeResult{Purged: int(tag.RowsAffected())})
}

// execer is the subset of *pgxpool.Pool refreshAfterPurge needs.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// purgeRefresher serialises the post-purge aggregate refreshes. At most one
// refresh loop runs at a time; purges that commit while it runs merge their
// windows (earliest start per source table) into one pending refresh, which
// the loop picks up when the current pass ends. Concurrent purges therefore
// never spawn overlapping refresh_continuous_aggregate calls.
type purgeRefresher struct {
	mu      sync.Mutex
	running bool
	pending map[string]time.Time
	run     func(pool execer, since map[string]time.Time) // refreshAfterPurge; swappable in tests
}

var purgeRefreshes = &purgeRefresher{run: refreshAfterPurge}

// schedule queues a refresh from since and starts the loop if it is idle.
func (r *purgeRefresher) schedule(pool execer, since map[string]time.Time) {
	if len(since) == 0 {
		return
	}
	r.mu.Lock()
	if r.pending == nil {
		r.pending = map[string]time.Time{}
	}
	for table, ts := range since {
		if cur, ok := r.pending[table]; !ok || ts.Before(cur) {
			r.pending[table] = ts
		}
	}
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()
	go r.loop(pool)
}

func (r *purgeRefresher) loop(pool execer) {
	for {
		r.mu.Lock()
		batch := r.pending
		r.pending = nil
		if len(batch) == 0 {
			r.running = false
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		r.run(pool, batch)
	}
}

// refreshAfterPurge re-materializes the continuous aggregates over the purged
// machines' time range so their rows leave the rollups too (the scheduled
// policies only look back a day). refresh_continuous_aggregate cannot run in a
// transaction, hence after commit; it only rewrites invalidated buckets, and the
// window starts at the purged machine's oldest surviving raw row, so no bucket
// whose raw data retention already dropped is touched. Failures are non-fatal.
func refreshAfterPurge(pool execer, since map[string]time.Time) {
	if len(since) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, src := range purgeCaggSources {
		from, ok := since[src.table]
		if !ok {
			continue
		}
		// refresh_continuous_aggregate only refreshes buckets wholly inside the
		// window; step back past the (hourly) bucket that contains `from`.
		from = from.Add(-2 * time.Hour)
		for _, view := range src.caggs {
			// View names are a fixed whitelist, not user input.
			if _, err := pool.Exec(ctx, "CALL refresh_continuous_aggregate('"+view+"', $1, NULL)", from); err != nil {
				log.Printf("purge: refresh %s: %v", view, err)
			}
		}
	}
}

package ingest

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// policyReloadInterval is the safety net behind the explicit Refresh() the admin
// write path performs: if a Refresh is ever missed (a failed write, a second API
// replica, a manual SQL edit), the fleet still converges within a minute instead
// of staying on a stale policy until the next restart.
const policyReloadInterval = 60 * time.Second

// Policies is an in-memory snapshot of the remediation policy, resolved per
// machine at ingest time. It follows the same pattern as Categories: the ingest
// path sees 100+ machines every 60 s and must never re-query the policy tables
// per batch. Safe for concurrent use; Refresh() after every admin write.
type Policies struct {
	pool *pgxpool.Pool

	mu   sync.RWMutex
	snap policySnapshot
}

type policySnapshot struct {
	revision     int64
	enabled      bool
	dryRun       bool
	excludedTags map[string]struct{}
	apps         []string
	startup      []string
}

// NewPolicies creates the cache and performs an initial load.
func NewPolicies(ctx context.Context, pool *pgxpool.Pool) (*Policies, error) {
	p := &Policies{pool: pool}
	if err := p.Refresh(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Refresh reloads the policy + its selected item ids from the database.
func (p *Policies) Refresh(ctx context.Context) error {
	var (
		snap     policySnapshot
		excluded []string
	)
	err := p.pool.QueryRow(ctx,
		`SELECT revision, enabled, dry_run, excluded_tags FROM remediation_policy WHERE id = 1`).
		Scan(&snap.revision, &snap.enabled, &snap.dryRun, &excluded)
	if err != nil {
		return err
	}
	snap.excludedTags = make(map[string]struct{}, len(excluded))
	for _, t := range excluded {
		snap.excludedTags[t] = struct{}{}
	}

	rows, err := p.pool.Query(ctx,
		`SELECT kind, item_id FROM remediation_policy_item ORDER BY kind, item_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return err
		}
		switch kind {
		case "startup":
			snap.startup = append(snap.startup, id)
		default: // "app"
			snap.apps = append(snap.apps, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
	return nil
}

// Run reloads the snapshot periodically until ctx is done (see
// policyReloadInterval). Start it as a goroutine from main.
func (p *Policies) Run(ctx context.Context) {
	t := time.NewTicker(policyReloadInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.Refresh(ctx); err != nil && ctx.Err() == nil {
				log.Printf("ingest: policy refresh: %v", err)
			}
		}
	}
}

// Revision returns the current fleet-wide policy revision (for the admin API).
func (p *Policies) Revision() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snap.revision
}

// Resolve returns the policy that applies to a machine carrying the given tags.
// The policy is fleet-wide: it applies unless machine.tags overlaps
// excluded_tags, in which case the machine gets an explicitly DISABLED policy
// rather than no policy at all — an agent that was previously reconciling must be
// told to stop, not left on its last instruction.
//
// The revision string carries an "-x" suffix for excluded machines. Tag edits go
// through PUT /api/admin/machines/{id}/labels and do NOT bump the fleet revision,
// so without the suffix a machine moving in or out of an excluded tag would keep
// matching the revision it already applied and never receive the change.
//
// Apps/Startup alias the cached snapshot; callers must not mutate them.
func (p *Policies) Resolve(tags []string) model.RemediationPolicy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.snap

	excluded := false
	for _, t := range tags {
		if _, ok := s.excludedTags[t]; ok {
			excluded = true
			break
		}
	}

	rev := strconv.FormatInt(s.revision, 10)
	if excluded {
		return model.RemediationPolicy{Revision: rev + "-x", Enabled: false, DryRun: true}
	}
	return model.RemediationPolicy{
		Revision: rev,
		Enabled:  s.enabled,
		DryRun:   s.dryRun,
		Apps:     s.apps,
		Startup:  s.startup,
	}
}

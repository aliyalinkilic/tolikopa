// Package actuator turns an accepted remediation policy into (very few, very
// bounded) local changes: removing an allowlisted Appx package, or disabling an
// allowlisted logon entry.
//
// AUTHORITY AND SAFETY, in the order the checks run:
//
//  1. the item must exist in the compiled catalog (internal/appcatalog);
//  2. the matching LOCAL kill switch must be on (config.json, GPO-provisioned);
//  3. the package must not be in the never-remove table (appcatalog.IsForbidden…);
//  4. it must actually be present / still enabled;
//  5. the policy must be enabled;
//  6. neither the server's dry_run nor the local enforce switch may veto it.
//
// Only then is anything actuated, one item at a time, never concurrently, and
// never more than a bounded number of times (see budget.go): on Windows 11 Pro
// ContentDeliveryManager reinstalls consumer apps, and an unbounded reconciler
// would fight the OS forever and flood the audit log.
//
// PRIVACY: attempts carry a catalog item_id, an action and a CODE. Never an OS
// message, never a path, never a user name — see model.RemediationAttempt.
package actuator

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// Outcomes reported back to the server (model.RemediationAttempt.Outcome).
const (
	OutcomeOK          = "ok"
	OutcomeFailed      = "failed"
	OutcomeDryRun      = "dry_run"
	OutcomeNotPresent  = "not_present"
	OutcomeNotAllowed  = "not_allowed"
	OutcomeBlocked     = "blocked"
	OutcomeTimeout     = "timeout"
	OutcomeUnknownItem = "unknown_item"
)

// Actions (model.RemediationAttempt.Action).
const (
	ActionRemove      = "remove"
	ActionDeprovision = "deprovision"
	ActionDisable     = "disable"
	ActionEnable      = "enable"
)

// Agent-defined error codes. They are shaped like HRESULTs so the panel can show
// them uniformly, but the 0xExxxxxxx range is ours and never comes from Windows.
const (
	ErrWinRTUnverified = "0xE0000001" // WinRT binding table incomplete (see winrt_constants_windows.go)
	ErrWorkerRefused   = "0xE0000002" // the removal worker refused the family (allowlist)
	ErrNoApprovedKey   = "0xE0000003" // RunOnce entry: no StartupApproved counterpart
	ErrWorkerIO        = "0xE0000004" // could not launch/talk to the removal worker
	ErrResolveFailed   = "0xE0000005" // could not ask Windows which packages are deployed
)

// Backend is the platform-specific half of the actuator.
type Backend interface {
	// ResolveApps answers, for each package family, which PackageFullNames are
	// actually deployed on this machine right now. A family with nothing deployed
	// is absent from the map. ok=false means the question could not be asked at
	// all (the platform read failed) — which is different from "nothing found".
	ResolveApps(ctx context.Context, families []string) (map[string][]string, bool)
	// RemoveApp removes the given instances of a package family for all users and
	// deprovisions the family so Windows Update / new profiles do not bring it
	// back. fullNames comes from ResolveApps and from nowhere else.
	RemoveApp(ctx context.Context, family string, fullNames []string) Result
	// SetStartup disables (enabled=false) or re-enables one catalog logon entry
	// across every observed registry location.
	SetStartup(ctx context.Context, targets []collector.StartupTarget, enabled bool) Result
}

// Result is one platform action's outcome plus a code (never a message).
type Result struct {
	Outcome   string
	ErrorCode string
}

// Actuator reconciles a policy against observed presence.
type Actuator struct {
	log     *slog.Logger
	backend Backend
	budget  *budgetStore

	mu             sync.Mutex // serializes Reconcile: never two changes at once
	startupTargets map[string][]collector.StartupTarget
	// liveApps is family (lower-cased) -> the PackageFullNames the deployment API
	// reported for it at the start of this pass, and liveOK whether that read
	// succeeded at all. The removal path reads nothing else.
	liveApps map[string][]string
	liveOK   bool
	// resolveRetryDelay is the pause before the one retry of a failed resolve.
	resolveRetryDelay time.Duration
}

// resolveRetryDelay gives AppXSvc time to finish whatever made it refuse the read
// (it is demand-started, and a Store update holds the deployment store briefly).
const resolveRetryDelay = 20 * time.Second

// New returns an Actuator with the platform backend for this OS.
func New(log *slog.Logger, exePath string) *Actuator {
	return &Actuator{
		log:            log,
		backend:        newBackend(log, exePath),
		budget:         newBudgetStore(log),
		startupTargets: map[string][]collector.StartupTarget{},
		liveApps:       map[string][]string{},

		resolveRetryDelay: resolveRetryDelay,
	}
}

// SetTargets hands the actuator the collector-internal detail it needs to act:
// the exact registry values observed for each catalog logon entry. None of this
// is ever transmitted.
//
// Apps have no counterpart here on purpose. They used to: the inventory passed
// down the PackageFullNames it had seen in the AppxAllUserStore registry view,
// and the removal worker was handed those names. That view is a deployment
// journal — it keeps one subkey per version ever deployed, keeps them after
// removal, and may not contain the version actually registered — so the names
// answered 0x80070490 (ERROR_NOT_FOUND) and nothing was ever removed. The
// removal path now resolves the family against the deployment API itself.
func (a *Actuator) SetTargets(startup []collector.StartupTarget) {
	st := make(map[string][]collector.StartupTarget, len(startup))
	for _, t := range startup {
		st[t.ItemID] = append(st[t.ItemID], t)
	}
	a.mu.Lock()
	a.startupTargets = st
	a.mu.Unlock()
}

// ResolveApps asks the platform which catalog app families are ACTUALLY deployed
// on this machine and caches the answer for this pass. Call it once, before
// Reconcile.
//
// It is the inventory's source of truth as well as the removal path's. The
// registry view the app collector reads is a deployment JOURNAL: it answers
// "this family was deployed here at some point", which over-reports presence (a
// removed package keeps its subkey) and cannot name the version that is actually
// registered. Reporting that to the panel as installed is simply wrong, and
// acting on it produced one pointless audit row per selected item per cycle.
func (a *Actuator) ResolveApps(ctx context.Context) {
	families := make([]string, 0, 64)
	for _, it := range appcatalog.Kind(appcatalog.KindApp) {
		families = append(families, it.Family)
	}

	live, ok := a.backend.ResolveApps(ctx, families)
	if !ok {
		// One retry. Fleet data showed the read failing on a machine for a single
		// pass and succeeding on the next one hours later — and every such pass
		// costs one failed/0xE0000005 audit row per selected app.
		a.log.Info("live package resolution failed; retrying once", "after", a.resolveRetryDelay.String())
		select {
		case <-ctx.Done():
		case <-time.After(a.resolveRetryDelay):
			live, ok = a.backend.ResolveApps(ctx, families)
		}
	}
	lower := make(map[string][]string, len(live))
	for f, names := range live {
		lower[strings.ToLower(f)] = names
	}

	a.mu.Lock()
	a.liveApps, a.liveOK = lower, ok
	a.mu.Unlock()

	if !ok {
		a.log.Warn("live package resolution unavailable this pass; app presence falls " +
			"back to the registry hint and app removals are deferred")
		return
	}
	a.log.Debug("live package resolution", "families_deployed", len(lower))
}

// CorrectPresence replaces the app collector's registry HINT with what the
// deployment API actually reports. Startup items pass through untouched: their
// registry source is the live one.
//
// When the resolution failed the hint is left exactly as it was — a read that
// did not happen must never be reported as "this machine is clean".
func (a *Actuator) CorrectPresence(present []model.RemediationItem) []model.RemediationItem {
	a.mu.Lock()
	live, ok := a.liveApps, a.liveOK
	a.mu.Unlock()
	if !ok {
		return present
	}

	out := present[:0:0]
	for _, it := range present {
		if it.Kind != appcatalog.KindApp {
			out = append(out, it)
			continue
		}
		item, known := appcatalog.Lookup(appcatalog.KindApp, it.ItemID)
		if !known {
			out = append(out, it)
			continue
		}
		if len(live[strings.ToLower(item.Family)]) > 0 {
			out = append(out, it)
			continue
		}
		// Deployed for nobody. A provisioned-store entry still matters — that is a
		// per-image registration a newly created profile would pick up — but it is
		// not deployed for any user, so a "user"/"both" scope becomes wrong.
		if it.Scope == appcatalog.ScopeProvisioned {
			out = append(out, it)
			continue
		}
		a.log.Debug("dropping app presence contradicted by the deployment API", "item_id", it.ItemID)
	}
	return out
}

// Reconcile evaluates the policy against what the collectors just found and
// returns one audit row per decision. It is safe to call with a nil policy (the
// agent has never been given one) — the result is then no attempts at all.
//
// Every action is performed serially inside the actuator lock: two removals must
// never overlap, and a slow one must never be started twice.
func (a *Actuator) Reconcile(ctx context.Context, p *model.RemediationPolicy, present []model.RemediationItem, allowed model.RemediationAllowed) []model.RemediationAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()

	if p == nil {
		return nil
	}

	// Presence index. For startup, "present" means present AND still enabled:
	// an already-disabled entry needs no work.
	appPresent := map[string]bool{}
	startupActive := map[string]bool{}
	for _, it := range present {
		switch it.Kind {
		case appcatalog.KindApp:
			appPresent[it.ItemID] = it.Present
		case appcatalog.KindStartup:
			startupActive[it.ItemID] = it.Present && (it.Enabled == nil || *it.Enabled)
		}
	}

	st := a.budget.load()
	var attempts []model.RemediationAttempt

	run := func(kind, action string, ids []string, kindAllowed bool, active map[string]bool) {
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			at, acted := a.decide(ctx, st, kind, action, id, kindAllowed, active[id], p, allowed)
			if acted {
				attempts = append(attempts, at)
			}
		}
	}

	// One line per pass instead of one audit row per item — see decide() rule 2.
	if !allowed.Apps && len(p.Apps) > 0 {
		a.log.Info("app removal not permitted on this machine (allow_app_removal=false); items skipped",
			"items", len(p.Apps), "policy_revision", p.Revision)
	}
	if !allowed.Startup && len(p.Startup) > 0 {
		a.log.Info("startup control not permitted on this machine (allow_startup_control=false); items skipped",
			"items", len(p.Startup), "policy_revision", p.Revision)
	}

	run(appcatalog.KindApp, ActionRemove, p.Apps, allowed.Apps, appPresent)
	run(appcatalog.KindStartup, ActionDisable, p.Startup, allowed.Startup, startupActive)

	a.budget.save(st)
	return attempts
}

// decide runs the fixed decision order for one selected item. acted=false means
// "emit no audit row at all", which happens in exactly two cases: the policy is
// disabled (rule 5), or the item is in its post-failure cooldown.
func (a *Actuator) decide(ctx context.Context, st *budgetState, kind, action, id string, kindAllowed, active bool, p *model.RemediationPolicy, allowed model.RemediationAllowed) (model.RemediationAttempt, bool) {
	row := model.RemediationAttempt{
		TS:     time.Now().UTC(),
		Kind:   kind,
		ItemID: id,
		Action: action,
	}
	key := kind + ":" + id

	// 1. Outside the compiled vocabulary: refuse locally, tell the server.
	item, ok := appcatalog.Lookup(kind, id)
	if !ok {
		row.Outcome = OutcomeUnknownItem
		return row, true
	}

	// 2. The local kill switch for this kind is off. The server cannot widen it.
	//
	// Deliberately emits NO audit row. The kill switch is MACHINE-level posture,
	// already carried by RemediationAllowed on every report and stored in
	// machine.remediation_allowed — one row per selected item per pass would repeat
	// that same fact 43 times every six hours on every opted-out machine and bury
	// the real outcomes in a 365-day audit table. Reconcile logs it once per pass.
	// (Found in the first pilot install: 86 rows within two passes on one PC.)
	if !kindAllowed {
		return row, false
	}

	// 3. Never-remove table. Unreachable in practice — the catalog unit test fails
	//    the build if an entry trips it — so reaching it means the binary's
	//    vocabulary and its safety table disagree. That is an ERROR, loudly.
	if kind == appcatalog.KindApp && appcatalog.IsForbiddenFamily(item.Family) {
		a.log.Error("refused protected package (catalog/forbidden-table disagreement)",
			"item_id", id, "family", item.Family)
		row.Outcome = OutcomeBlocked
		return row, true
	}

	// 4. Nothing to do: not installed, or the logon entry is already disabled.
	if !active {
		delete(st.Items, key) // a clean item forgets its failure history
		row.Outcome = OutcomeNotPresent
		return row, true
	}

	// 5. The policy itself is switched off: report nothing at all for this item.
	if !p.Enabled {
		return row, false
	}

	// 6. Either side may veto actuation and ask for an evaluation only.
	if p.DryRun || !allowed.Enforce {
		row.Outcome = OutcomeDryRun
		return row, true
	}

	// 7. Actuate — but only within the per-item attempt budget.
	if until, cooling := st.cooling(key, row.TS); cooling {
		a.log.Info("hygiene item in cooldown, skipping",
			"item_id", id, "kind", kind, "until", until.Format(time.RFC3339))
		return row, false
	}

	start := time.Now()
	var res Result
	switch kind {
	case appcatalog.KindApp:
		switch names := a.liveApps[strings.ToLower(item.Family)]; {
		case len(names) > 0:
			res = a.backend.RemoveApp(ctx, item.Family, names)
		case a.liveOK:
			// The deployment API says nothing is deployed. Do not spawn a removal
			// worker just to be told the same thing.
			res = Result{Outcome: OutcomeNotPresent}
		default:
			// The read failed. Acting blind would mean guessing at package
			// identities again, which is the bug this path was rewritten for.
			res = Result{Outcome: OutcomeFailed, ErrorCode: ErrResolveFailed}
		}
	case appcatalog.KindStartup:
		res = a.backend.SetStartup(ctx, a.startupTargets[id], false)
	}
	row.Outcome, row.ErrorCode = res.Outcome, res.ErrorCode
	row.DurationMs = int(time.Since(start) / time.Millisecond)

	// not_present counts as converged: the desired state is that the item is
	// gone. Counting it as a failure would spend the item's three attempts and
	// park it in a 24 h cooldown for succeeding. blocked gets its own long
	// back-off (see budget.go).
	st.record(key, row.TS, res.Outcome)
	a.log.Info("hygiene action",
		"kind", kind, "item_id", id, "action", action,
		"outcome", row.Outcome, "code", row.ErrorCode, "ms", row.DurationMs)
	return row, true
}

// EnableStartup is the inverse of the disable action, so every change Tolikopa makes
// is reversible from the same code path that made it.
//
// It is deliberately NOT called by Reconcile: the policy is a deny-list, and an
// item dropping out of it means "stop managing this", not "switch it back on" —
// the entry may have been disabled by the user themselves.
//
// It takes `allowed` even though it has no caller yet: this is an actuation entry
// point, and the kill-switch check must exist BEFORE someone wires it to an API or
// a "geri al" button. A guard added later is a guard that was missing in between.
func (a *Actuator) EnableStartup(ctx context.Context, itemID string, allowed model.RemediationAllowed) model.RemediationAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()

	row := model.RemediationAttempt{
		TS:     time.Now().UTC(),
		Kind:   appcatalog.KindStartup,
		ItemID: itemID,
		Action: ActionEnable,
	}
	if _, ok := appcatalog.Lookup(appcatalog.KindStartup, itemID); !ok {
		row.Outcome = OutcomeUnknownItem
		return row
	}
	if !allowed.Startup || !allowed.Enforce {
		row.Outcome = OutcomeNotAllowed
		return row
	}
	targets := a.startupTargets[itemID]
	if len(targets) == 0 {
		row.Outcome = OutcomeNotPresent
		return row
	}
	start := time.Now()
	res := a.backend.SetStartup(ctx, targets, true)
	row.Outcome, row.ErrorCode = res.Outcome, res.ErrorCode
	row.DurationMs = int(time.Since(start) / time.Millisecond)
	return row
}

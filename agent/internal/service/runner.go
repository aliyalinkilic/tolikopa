// Package service holds the OS-agnostic run loop plus the Windows-service and
// Linux-systemd lifecycle glue.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/aliyalinkilic/tolikopa/agent/internal/actuator"
	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
	"github.com/aliyalinkilic/tolikopa/agent/internal/policy"
	"github.com/aliyalinkilic/tolikopa/agent/internal/sender"
)

// Service identity. Description is user-facing (services.msc / systemctl) and is
// therefore Turkish, per project convention.
const (
	Name        = "Tolikopa"
	DisplayName = "Tolikopa Agent"
	Description = "Tolikopa sistem izleme ajanı"
)

// RunForeground runs the collection loop in the foreground until SIGINT/SIGTERM.
// Used by the `run` subcommand and by the Linux systemd unit.
func RunForeground(cfg *config.Config, log *slog.Logger, version string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tracker := collector.NewSessionTracker()
	return NewRunner(cfg, log, tracker, version).Run(ctx)
}

// Runner owns the collection/send loop. It is created by the platform service
// (which also feeds session-change events into the shared SessionTracker).
type Runner struct {
	cfg     *config.Config
	log     *slog.Logger
	session *collector.SessionTracker
	version string
}

// NewRunner constructs a Runner. session is shared with the service so that
// Windows SESSIONCHANGE notifications update the same lock state this loop reads.
func NewRunner(cfg *config.Config, log *slog.Logger, session *collector.SessionTracker, version string) *Runner {
	return &Runner{cfg: cfg, log: log, session: session, version: version}
}

// Run drives collection until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	agentID, err := loadOrCreateAgentID()
	if err != nil {
		return err
	}
	machine := collector.MachineInfo(agentID, r.version)
	r.log.Info("agent starting",
		"machine_id", machine.MachineID, "hostname", machine.Hostname,
		"os", machine.OS, "kind", machine.Kind, "version", r.version)

	// Idle threshold reaches the tracker from config here; until the helper
	// reports input recency the tracker keeps today's behavior ("active").
	r.session.SetIdleThreshold(r.cfg.IdleThresholdSec)

	coll := collector.New(r.cfg, r.log)
	coll.Prime()
	health := collector.NewHealthCollector(r.cfg, r.log)
	events := collector.NewEventCollector(r.log)

	snd, err := sender.New(r.cfg, machine, r.log)
	if err != nil {
		return err
	}
	// Endpoint hygiene: the policy channel is wired BEFORE the sender starts, so a
	// directive returned on the very first POST is not dropped.
	policyStore := policy.NewStore()
	policyBump := make(chan struct{}, 1)
	snd.SetPolicyHandler(r.policyHandler(policyStore, policyBump))
	go snd.Run(ctx)

	vitals := time.Duration(r.cfg.Intervals.VitalsSec) * time.Second
	if vitals <= 0 {
		vitals = 15 * time.Second
	}
	ratio := r.cfg.Intervals.SampleSec / r.cfg.Intervals.VitalsSec
	if ratio < 1 {
		ratio = 1
	}

	go r.smartLoop(ctx, health, snd)
	go r.updateLoop(ctx, collector.NewUpdateCollector(r.cfg, r.log), snd)
	go r.batteryLoop(ctx, collector.NewBatteryCollector(r.cfg, r.log), snd)

	exe, _ := os.Executable()
	// In-session foreground app-usage + idle helper (Windows only; no-op elsewhere).
	if exe != "" {
		go runForegroundManager(ctx, exe, snd, r.session, r.log)
	}

	go r.hygieneLoop(ctx, collector.NewAppInventoryCollector(r.log),
		collector.NewStartupCollector(r.log), actuator.New(r.log, exe), policyStore, snd, policyBump)

	// Emit one full sample immediately so the machine appears in the fleet fast.
	r.collect(coll, snd, events, true)

	ticker := time.NewTicker(vitals)
	defer ticker.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			r.log.Info("agent stopping")
			return nil
		case <-ticker.C:
			n++
			full := n%ratio == 0
			r.collect(coll, snd, events, full)
		}
	}
}

// collect gathers one sample (light, or full with disks + top-N + events).
func (r *Runner) collect(coll *collector.Collector, snd *sender.Sender, events collector.EventCollector, full bool) {
	s := coll.Vitals(r.session.Current())
	if full {
		coll.Full(&s)
		snd.AddEvents(events.Collect())
	}
	snd.AddSample(s)
}

// smartLoop polls SMART health on the low smart_min cadence.
func (r *Runner) smartLoop(ctx context.Context, health collector.HealthCollector, snd *sender.Sender) {
	interval := time.Duration(r.cfg.Intervals.SmartMin) * time.Minute
	if interval <= 0 {
		interval = 45 * time.Minute
	}
	// First read shortly after startup (not at t=0, to avoid blocking init).
	select {
	case <-ctx.Done():
		return
	case <-time.After(90 * time.Second):
		snd.AddHealth(health.Collect())
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snd.AddHealth(health.Collect())
		}
	}
}

// batteryLoop polls battery/power state on a moderate cadence (charge % is
// semi-live; health changes slowly). First read shortly after startup.
func (r *Runner) batteryLoop(ctx context.Context, bat collector.BatteryCollector, snd *sender.Sender) {
	const cadence = 10 * time.Minute
	select {
	case <-ctx.Done():
		return
	case <-time.After(75 * time.Second):
		snd.SetBattery(bat.Collect())
	}
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snd.SetBattery(bat.Collect())
		}
	}
}

// updateLoop polls the machine's Windows Update posture on a low cadence. It is
// read-only (local WUA cache + registry flags; never an online scan) so it adds
// no WSUS/WU network load. The first read is a couple of minutes after startup so
// it never blocks init.
func (r *Runner) updateLoop(ctx context.Context, upd collector.UpdateCollector, snd *sender.Sender) {
	const cadence = 6 * time.Hour
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
		snd.SetUpdateStatus(upd.Collect())
	}
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snd.SetUpdateStatus(upd.Collect())
		}
	}
}

// hygieneLoop reports the endpoint-hygiene posture and reconciles the stored
// policy. Shaped like updateLoop: a first pass a few minutes after startup (never
// blocking init), then every hygiene_hours.
//
// The report is produced on EVERY pass, including when every kill switch is off:
// the inventory is read-only and always-on — only actuation is gated. A machine
// therefore shows up in the panel with its bloatware posture long before anyone
// decides to act on it.
func (r *Runner) hygieneLoop(ctx context.Context, apps collector.AppInventoryCollector,
	startup collector.StartupCollector, act *actuator.Actuator, store *policy.Store,
	snd *sender.Sender, bump <-chan struct{}) {

	interval := time.Duration(r.cfg.HygieneHours) * time.Hour
	if interval <= 0 {
		interval = 6 * time.Hour
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Minute):
		r.runHygiene(ctx, apps, startup, act, store, snd)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runHygiene(ctx, apps, startup, act, store, snd)
		case <-bump:
			// A new policy just arrived on the live path. Settle briefly (the
			// server may be mid-rollout and the sender may post again shortly),
			// then reconcile instead of waiting up to hygiene_hours.
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			r.runHygiene(ctx, apps, startup, act, store, snd)
		}
	}
}

// runHygiene is one pass: inventory → reconcile → attach the report.
func (r *Runner) runHygiene(ctx context.Context, apps collector.AppInventoryCollector,
	startup collector.StartupCollector, act *actuator.Actuator, store *policy.Store,
	snd *sender.Sender) {

	// This pass walks the registry and may spawn a child process. A panic here
	// must never take down a service that runs as SYSTEM fleet-wide: degrade to
	// "no hygiene report this cycle".
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("hygiene pass panicked", "recover", fmt.Sprint(rec))
		}
	}()

	// Ask Windows which catalog packages are actually deployed here. This is ONE
	// short-lived child process for the whole catalog, and it is the source of
	// truth for BOTH the reported inventory and the removal targets: the registry
	// view apps.Collect() reads is a deployment journal that over-reports presence
	// and cannot name the version that is really registered.
	act.ResolveApps(ctx)

	present := apps.Collect()
	present = act.CorrectPresence(present)
	present = append(present, startup.Collect()...)

	// Hand the actuator the collector-internal detail (the exact registry values
	// behind each logon entry). It never reaches the wire. Apps need no equivalent:
	// ResolveApps above already holds their live PackageFullNames.
	var startupTargets []collector.StartupTarget
	if t, ok := startup.(collector.StartupTargeter); ok {
		startupTargets = t.Targets()
	}
	act.SetTargets(startupTargets)

	state := store.Current()
	allowed := model.RemediationAllowed{
		Apps:    r.cfg.AllowAppRemoval,
		Startup: r.cfg.AllowStartupControl,
		Enforce: r.cfg.EnforceActions,
	}

	attempts := act.Reconcile(ctx, &state.Policy, present, allowed)
	attempts = append(attempts, unknownItemAttempts(state.Rejected)...)

	snd.SetRemediation(&model.RemediationReport{
		CatalogVersion: appcatalog.Version(),
		PolicyRevision: state.Policy.Revision,
		Allowed:        allowed,
		Present:        present,
		Attempts:       attempts,
		CheckedAt:      time.Now().UTC(),
	})
	r.log.Debug("hygiene pass complete",
		"present", len(present), "attempts", len(attempts), "revision", state.Policy.Revision)
}

// unknownItemAttempts reports the item_ids the server selected that this agent's
// compiled catalog does not contain. They are re-reported on every pass: the
// server must keep seeing that this agent cannot honour them, not just once.
func unknownItemAttempts(rejected []string) []model.RemediationAttempt {
	now := time.Now().UTC()
	out := make([]model.RemediationAttempt, 0, len(rejected))
	for _, r := range rejected {
		kind, id, ok := policy.SplitRejected(r)
		if !ok {
			continue
		}
		action := actuator.ActionRemove
		if kind == appcatalog.KindStartup {
			action = actuator.ActionDisable
		}
		out = append(out, model.RemediationAttempt{
			TS: now, Kind: kind, ItemID: id, Action: action,
			Outcome: actuator.OutcomeUnknownItem,
		})
	}
	return out
}

// policyHandler validates and persists a policy handed back by the ingest
// endpoint, then nudges the hygiene loop. It runs on the sender goroutine, so it
// only touches the local state file.
func (r *Runner) policyHandler(store *policy.Store, bump chan<- struct{}) func(*model.RemediationPolicy) {
	return func(p *model.RemediationPolicy) {
		if p == nil {
			return
		}
		accepted, rejected := policy.Validate(p)
		if accepted.Revision == "" {
			// Wholesale refusal: no revision, or a selection list past the cap.
			r.log.Warn("remediation policy refused",
				"revision", p.Revision, "apps", len(p.Apps), "startup", len(p.Startup))
			return
		}
		if len(rejected) > 0 {
			r.log.Warn("remediation policy names items outside this agent's catalog",
				"revision", accepted.Revision, "unknown", len(rejected),
				"catalog_version", appcatalog.Version())
		}

		cur := store.Current()
		if cur.Policy.Revision == accepted.Revision {
			return // already applied; nothing changed
		}
		if err := store.Save(policy.State{
			Policy:    accepted,
			Rejected:  rejected,
			AppliedAt: time.Now().UTC(),
		}); err != nil {
			r.log.Error("policy store write failed", "err", err.Error())
			return
		}
		r.log.Info("remediation policy accepted",
			"revision", accepted.Revision, "enabled", accepted.Enabled, "dry_run", accepted.DryRun,
			"apps", len(accepted.Apps), "startup", len(accepted.Startup))
		select {
		case bump <- struct{}{}:
		default:
		}
	}
}

// loadOrCreateAgentID returns the persisted stable UUID, generating it on first run.
func loadOrCreateAgentID() (string, error) {
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	path := config.AgentIDPath()
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	id := uuid.NewString()
	if err := os.WriteFile(path, []byte(id), 0o640); err != nil {
		return "", err
	}
	return id, nil
}

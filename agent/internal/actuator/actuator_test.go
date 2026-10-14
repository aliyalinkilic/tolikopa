package actuator

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// fakeBackend records what the decision logic actually asked for.
type fakeBackend struct {
	removes  []string
	targets  [][]string // the PackageFullNames each removal was handed
	startups []string
	result   Result

	// live is what the deployment API "reports"; resolveFails makes the read
	// itself fail, which is a different thing from reporting nothing.
	live        map[string][]string
	resolveFail bool
	// resolveFailFirst fails that many reads and then recovers; resolves counts them.
	resolveFailFirst int
	resolves         int
}

func (f *fakeBackend) ResolveApps(_ context.Context, _ []string) (map[string][]string, bool) {
	f.resolves++
	if f.resolveFail || f.resolves <= f.resolveFailFirst {
		return nil, false
	}
	return f.live, true
}

func (f *fakeBackend) RemoveApp(_ context.Context, family string, fullNames []string) Result {
	f.removes = append(f.removes, family)
	f.targets = append(f.targets, fullNames)
	return f.result
}

func (f *fakeBackend) SetStartup(_ context.Context, targets []collector.StartupTarget, _ bool) Result {
	for _, t := range targets {
		f.startups = append(f.startups, t.ItemID)
	}
	return f.result
}

func newTestActuator(t *testing.T, be Backend) *Actuator {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Actuator{
		log:            log,
		backend:        be,
		budget:         &budgetStore{log: log, path: filepath.Join(t.TempDir(), "budget.json")},
		startupTargets: map[string][]collector.StartupTarget{},
		liveApps:       map[string][]string{},
	}
}

// deployed hands the actuator a live-resolution result for one catalog family,
// the way ResolveApps would after asking the deployment API.
func deployed(t *testing.T, a *Actuator, be *fakeBackend, itemIDs ...string) {
	t.Helper()
	be.live = map[string][]string{}
	for _, id := range itemIDs {
		item, ok := appcatalog.Lookup(appcatalog.KindApp, id)
		if !ok {
			t.Fatalf("catalog has no app %q", id)
		}
		be.live[item.Family] = []string{item.Family + "::deployed"}
	}
	a.ResolveApps(context.Background())
}

func presentApp(id string) model.RemediationItem {
	return model.RemediationItem{Kind: appcatalog.KindApp, ItemID: id, Present: true, Scope: appcatalog.ScopeBoth}
}

func presentStartup(id string, enabled bool) model.RemediationItem {
	e := enabled
	return model.RemediationItem{Kind: appcatalog.KindStartup, ItemID: id, Present: true, Enabled: &e}
}

func allOn() model.RemediationAllowed {
	return model.RemediationAllowed{Apps: true, Startup: true, Enforce: true}
}

// outcomes indexes the returned rows by item_id.
func outcomes(rows []model.RemediationAttempt) map[string]string {
	m := map[string]string{}
	for _, r := range rows {
		m[r.ItemID] = r.Outcome
	}
	return m
}

func TestDecisionOrder(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	a.SetTargets(
		[]collector.StartupTarget{{ItemID: "startup.spotify", ValueName: "Spotify", Approved: "Run", Enabled: true}},
	)
	deployed(t, a, be, "weather")

	p := &model.RemediationPolicy{
		Revision: "r1", Enabled: true,
		Apps:    []string{"weather", "no-such-item", "news"},
		Startup: []string{"startup.spotify", "startup.steam"},
	}
	present := []model.RemediationItem{
		presentApp("weather"),
		presentStartup("startup.spotify", true),
		presentStartup("startup.steam", false), // present but already disabled
	}

	got := outcomes(a.Reconcile(context.Background(), p, present, allOn()))
	want := map[string]string{
		"weather":         OutcomeOK,          // present, allowed, enforced
		"no-such-item":    OutcomeUnknownItem, // rule 1
		"news":            OutcomeNotPresent,  // rule 4 (in catalog, not installed)
		"startup.spotify": OutcomeOK,
		"startup.steam":   OutcomeNotPresent, // already disabled
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("outcome[%s] = %q, want %q", id, got[id], w)
		}
	}
	if len(be.removes) != 1 || be.removes[0] != "Microsoft.BingWeather_8wekyb3d8bbwe" {
		t.Errorf("removes = %v, want exactly the weather family", be.removes)
	}
	if len(be.startups) != 1 || be.startups[0] != "startup.spotify" {
		t.Errorf("startups = %v, want exactly startup.spotify", be.startups)
	}
}

func TestKillSwitchesBeatEverything(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	p := &model.RemediationPolicy{Revision: "r1", Enabled: true,
		Apps: []string{"weather"}, Startup: []string{"startup.spotify"}}
	present := []model.RemediationItem{presentApp("weather"), presentStartup("startup.spotify", true)}

	// Both kind switches off: nothing is actuated AND no audit row is produced.
	//
	// Emitting a row per item here looks harmless but is not: the kill switch is
	// machine-level posture (already on every report as RemediationAllowed and in
	// machine.remediation_allowed), so per-item rows repeat one fact N times every
	// pass on every opted-out machine, in a table kept for a year. The first pilot
	// install produced 86 such rows within two passes on a single PC.
	rows := a.Reconcile(context.Background(), p, present, model.RemediationAllowed{Enforce: true})
	if len(rows) != 0 {
		t.Errorf("kind switches off: got %d audit rows, want none (%v)", len(rows), outcomes(rows))
	}
	if len(be.removes) != 0 || len(be.startups) != 0 {
		t.Errorf("kind switches off: backend was called (removes=%v startups=%v)", be.removes, be.startups)
	}

	// Kinds allowed but enforcement off: dry_run, and nothing is touched.
	got := outcomes(a.Reconcile(context.Background(), p, present,
		model.RemediationAllowed{Apps: true, Startup: true}))
	if got["weather"] != OutcomeDryRun || got["startup.spotify"] != OutcomeDryRun {
		t.Errorf("enforce off: got %v, want dry_run for both", got)
	}

	// The server's own dry_run flag is equally sufficient.
	dry := *p
	dry.DryRun = true
	got = outcomes(a.Reconcile(context.Background(), &dry, present, allOn()))
	if got["weather"] != OutcomeDryRun {
		t.Errorf("policy dry_run: got %v, want dry_run", got)
	}

	if len(be.removes) != 0 || len(be.startups) != 0 {
		t.Fatalf("backend was called despite a veto: %v / %v", be.removes, be.startups)
	}
}

func TestDisabledPolicyEmitsNothingForActionableItems(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	p := &model.RemediationPolicy{Revision: "r1", Enabled: false,
		Apps: []string{"weather", "no-such-item"}}
	present := []model.RemediationItem{presentApp("weather")}

	rows := a.Reconcile(context.Background(), p, present, allOn())
	got := outcomes(rows)
	if _, ok := got["weather"]; ok {
		t.Errorf("a disabled policy must emit no row for an actionable item, got %v", got)
	}
	// Rules 1-4 still resolve: the server learns its selection is unusable here.
	if got["no-such-item"] != OutcomeUnknownItem {
		t.Errorf("unknown item should still be reported, got %v", got)
	}
	if len(be.removes) != 0 {
		t.Fatal("disabled policy actuated something")
	}
}

func TestNilPolicyDoesNothing(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	if rows := a.Reconcile(context.Background(), nil, []model.RemediationItem{presentApp("weather")}, allOn()); len(rows) != 0 {
		t.Errorf("nil policy produced %d rows", len(rows))
	}
}

// TestAttemptBudget is the ContentDeliveryManager guard: after maxAttempts
// failures the item goes quiet for 24h instead of being retried every cycle.
func TestAttemptBudget(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeFailed, ErrorCode: "0x80073CFA"}}
	a := newTestActuator(t, be)
	a.SetTargets(nil)
	deployed(t, a, be, "weather")

	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	present := []model.RemediationItem{presentApp("weather")}

	for i := 0; i < maxAttempts; i++ {
		rows := a.Reconcile(context.Background(), p, present, allOn())
		if len(rows) != 1 || rows[0].Outcome != OutcomeFailed {
			t.Fatalf("attempt %d: got %+v, want one failed row", i+1, rows)
		}
	}
	if len(be.removes) != maxAttempts {
		t.Fatalf("backend called %d times, want %d", len(be.removes), maxAttempts)
	}

	// In cooldown: no call, and no audit row at all.
	rows := a.Reconcile(context.Background(), p, present, allOn())
	if len(rows) != 0 {
		t.Errorf("cooldown must emit no rows, got %+v", rows)
	}
	if len(be.removes) != maxAttempts {
		t.Errorf("backend called during cooldown (%d calls)", len(be.removes))
	}

	// Cooldown expiry gives the item a fresh budget.
	st := a.budget.load()
	st.Items["app:weather"].CooldownUntil = time.Now().Add(-time.Minute)
	a.budget.save(st)
	if rows = a.Reconcile(context.Background(), p, present, allOn()); len(rows) != 1 {
		t.Errorf("after cooldown expiry, expected one row, got %+v", rows)
	}
}

// TestBlockedBacksOffForWeeks pins the fleet finding behind it: a Windows refusal
// (0x80073CFA) was retried three times a day forever, on every item of the
// affected machine. One refusal now parks the item for a week, and the back-off
// doubles up to four weeks — but a later success still clears it.
func TestBlockedBacksOffForWeeks(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeBlocked, ErrorCode: "0x80073CFA"}}
	a := newTestActuator(t, be)
	a.SetTargets(nil)
	deployed(t, a, be, "weather")
	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	present := []model.RemediationItem{presentApp("weather")}

	want := []time.Duration{7 * 24 * time.Hour, 14 * 24 * time.Hour, 28 * 24 * time.Hour, 28 * 24 * time.Hour}
	for i, backoff := range want {
		rows := a.Reconcile(context.Background(), p, present, allOn())
		if len(rows) != 1 || rows[0].Outcome != OutcomeBlocked {
			t.Fatalf("refusal %d: got %+v, want one blocked row", i+1, rows)
		}
		// The very next pass is silent: no second attempt the same day.
		if rows := a.Reconcile(context.Background(), p, present, allOn()); len(rows) != 0 {
			t.Fatalf("refusal %d: retried inside the back-off: %+v", i+1, rows)
		}
		b := a.budget.load().Items["app:weather"]
		if b == nil {
			t.Fatalf("refusal %d: no budget entry", i+1)
		}
		if got := b.CooldownUntil.Sub(b.LastAttempt); got != backoff {
			t.Errorf("refusal %d: back-off %v, want %v", i+1, got, backoff)
		}
		// Expire the back-off; the streak must survive so the next one escalates.
		st := a.budget.load()
		st.Items["app:weather"].CooldownUntil = time.Now().Add(-time.Minute)
		a.budget.save(st)
	}
	if len(be.removes) != len(want) {
		t.Errorf("backend called %d times, want %d", len(be.removes), len(want))
	}

	be.result = Result{Outcome: OutcomeOK}
	a.Reconcile(context.Background(), p, present, allOn())
	if st := a.budget.load(); st.Items["app:weather"] != nil {
		t.Errorf("success did not clear the blocked streak: %+v", st.Items["app:weather"])
	}
}

// TestSuccessClearsBudget: a successful action forgets the item's history, so a
// package Windows keeps re-adding gets its full budget back each time.
func TestSuccessClearsBudget(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeFailed}}
	a := newTestActuator(t, be)
	a.SetTargets(nil)
	deployed(t, a, be, "weather")
	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	present := []model.RemediationItem{presentApp("weather")}

	a.Reconcile(context.Background(), p, present, allOn())
	if st := a.budget.load(); st.Items["app:weather"] == nil || st.Items["app:weather"].Attempts != 1 {
		t.Fatalf("failure was not recorded: %+v", st.Items)
	}
	be.result = Result{Outcome: OutcomeOK}
	a.Reconcile(context.Background(), p, present, allOn())
	if st := a.budget.load(); st.Items["app:weather"] != nil {
		t.Errorf("success did not clear the budget entry: %+v", st.Items["app:weather"])
	}
}

// TestAbsentPackageIsNotAFailure pins the bug this path was rewritten for. The
// inventory's registry view says "weather is here"; the deployment API says
// nothing of that family is deployed. The API wins: the outcome is not_present,
// no removal worker is spawned, and the attempt budget is not spent — otherwise
// the item would be parked in a 24 h cooldown for already being in the desired
// state.
func TestAbsentPackageIsNotAFailure(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	deployed(t, a, be) // the read succeeded; nothing is deployed

	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	present := []model.RemediationItem{presentApp("weather")}

	for i := 0; i < maxAttempts+2; i++ {
		rows := a.Reconcile(context.Background(), p, present, allOn())
		if len(rows) != 1 || rows[0].Outcome != OutcomeNotPresent {
			t.Fatalf("pass %d: got %+v, want one not_present row", i+1, rows)
		}
		if rows[0].ErrorCode != "" {
			t.Errorf("pass %d: not_present carried error code %q", i+1, rows[0].ErrorCode)
		}
	}
	if len(be.removes) != 0 {
		t.Errorf("a removal worker was spawned for a package that is not deployed (%d times)", len(be.removes))
	}
	if st := a.budget.load(); st.Items["app:weather"] != nil {
		t.Errorf("not_present spent the attempt budget: %+v", st.Items["app:weather"])
	}
}

// TestRemovalTargetsComeFromTheLiveResolution is the other half of that bug: the
// worker must be handed the PackageFullNames the deployment API reported, and
// only those.
func TestRemovalTargetsComeFromTheLiveResolution(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)

	item, ok := appcatalog.Lookup(appcatalog.KindApp, "weather")
	if !ok {
		t.Fatal("catalog has no app \"weather\"")
	}
	want := []string{
		item.Family + "::v1",
		item.Family + "::v2",
	}
	be.live = map[string][]string{item.Family: want}
	a.ResolveApps(context.Background())

	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	rows := a.Reconcile(context.Background(), p, []model.RemediationItem{presentApp("weather")}, allOn())

	if len(rows) != 1 || rows[0].Outcome != OutcomeOK {
		t.Fatalf("got %+v, want one ok row", rows)
	}
	if len(be.targets) != 1 || len(be.targets[0]) != len(want) {
		t.Fatalf("removal targets = %v, want %v", be.targets, want)
	}
	for i, got := range be.targets[0] {
		if got != want[i] {
			t.Errorf("target[%d] = %q, want %q", i, got, want[i])
		}
	}
}

// TestResolutionFailureDoesNotActBlind: if the deployment API could not be asked,
// the agent must not fall back to guessing package identities — that guess is
// exactly what made every removal answer 0x80070490.
func TestResolutionFailureDoesNotActBlind(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}, resolveFail: true}
	a := newTestActuator(t, be)
	a.ResolveApps(context.Background())

	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	rows := a.Reconcile(context.Background(), p, []model.RemediationItem{presentApp("weather")}, allOn())

	if len(rows) != 1 || rows[0].Outcome != OutcomeFailed || rows[0].ErrorCode != ErrResolveFailed {
		t.Errorf("got %+v, want one failed/%s row", rows, ErrResolveFailed)
	}
	if len(be.removes) != 0 {
		t.Error("removal attempted without knowing what is deployed")
	}
}

// TestResolutionIsRetriedOnce: a read that fails once and then answers must not
// cost the pass — and a read that keeps failing is asked exactly twice, not more.
func TestResolutionIsRetriedOnce(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}, resolveFailFirst: 1}
	a := newTestActuator(t, be)
	deployed(t, a, be, "weather")

	p := &model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: []string{"weather"}}
	rows := a.Reconcile(context.Background(), p, []model.RemediationItem{presentApp("weather")}, allOn())
	if len(rows) != 1 || rows[0].Outcome != OutcomeOK {
		t.Errorf("got %+v, want one ok row after the retry", rows)
	}

	be = &fakeBackend{resolveFail: true}
	a = newTestActuator(t, be)
	a.ResolveApps(context.Background())
	if be.resolves != 2 {
		t.Errorf("resolve asked %d times, want 2", be.resolves)
	}
}

// TestCorrectPresenceTrustsTheDeploymentAPI: the registry hint over-reports, so a
// family the API does not report is dropped from the inventory — unless it is a
// provisioned-store entry, which is a per-image registration the API's per-user
// view would not cover.
func TestCorrectPresenceTrustsTheDeploymentAPI(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}}
	a := newTestActuator(t, be)
	deployed(t, a, be, "weather")

	provisioned := presentApp("news")
	provisioned.Scope = appcatalog.ScopeProvisioned

	got := a.CorrectPresence([]model.RemediationItem{
		presentApp("weather"),                   // deployed  -> kept
		presentApp("solitaire"),                 // journal leftover -> dropped
		provisioned,                             // per-image registration -> kept
		presentStartup("startup.spotify", true), // not an app -> untouched
	})

	want := map[string]bool{"weather": true, "news": true, "startup.spotify": true}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %d items", got, len(want))
	}
	for _, it := range got {
		if !want[it.ItemID] {
			t.Errorf("unexpected item %q survived presence correction", it.ItemID)
		}
	}
}

// TestCorrectPresenceKeepsTheHintWhenTheReadFailed: a read that did not happen
// must never be reported as "this machine is clean".
func TestCorrectPresenceKeepsTheHintWhenTheReadFailed(t *testing.T) {
	be := &fakeBackend{result: Result{Outcome: OutcomeOK}, resolveFail: true}
	a := newTestActuator(t, be)
	a.ResolveApps(context.Background())

	in := []model.RemediationItem{presentApp("weather"), presentApp("solitaire")}
	if got := a.CorrectPresence(in); len(got) != len(in) {
		t.Errorf("got %d items, want the %d unchanged hint items", len(got), len(in))
	}
}

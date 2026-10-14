package actuator

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
)

// Per-item attempt budget.
//
// WHY THIS EXISTS: on Windows 11 Pro, ContentDeliveryManager re-installs consumer
// apps (Solitaire, TikTok, Spotify…) into user profiles, and Windows Update
// re-adds some inbox apps at feature-update time. Without a bound, the agent
// would remove a package, Windows would put it back, the agent would remove it
// again — forever, once per hygiene cycle, writing an audit row every time. The
// budget turns that into "try at most 3 times, then leave it alone for a day".
//
// The state lives next to the policy in the agent state directory so an admin can
// inspect (or delete) both together.
//
// A "blocked" outcome (Windows refused the removal — 0x80073CFA, or the worker's
// own allowlist) is different: retrying it three times a day changes nothing and
// wrote ~3 audit rows per item per day on every such machine. It skips the
// three-strike budget and backs off for a week, doubling on each further refusal
// up to four weeks. It is deliberately NOT permanent: 0x80073CFA is the generic
// ERROR_REMOVE_FAILED, and a machine whose deployment store gets repaired should
// eventually converge without anyone touching its state file.
const (
	maxAttempts        = 3
	cooldown           = 24 * time.Hour
	blockedCooldown    = 7 * 24 * time.Hour
	blockedCooldownMax = 28 * 24 * time.Hour
)

// budgetState is the on-disk shape of state/hygiene_budget.json.
type budgetState struct {
	Items map[string]*itemBudget `json:"items"`
}

// itemBudget tracks one "kind:item_id".
type itemBudget struct {
	Attempts      int       `json:"attempts"`
	LastAttempt   time.Time `json:"last_attempt"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	// BlockedStreak counts consecutive "blocked" refusals; it survives cooldown
	// expiry so the back-off can escalate, and only a converged outcome clears it.
	BlockedStreak int `json:"blocked_streak,omitempty"`
}

// cooling reports whether the item is inside its post-failure cooldown window.
func (s *budgetState) cooling(key string, now time.Time) (time.Time, bool) {
	b := s.Items[key]
	if b == nil || b.CooldownUntil.IsZero() {
		return time.Time{}, false
	}
	if now.Before(b.CooldownUntil) {
		return b.CooldownUntil, true
	}
	// Cooldown expired: the item gets a fresh budget.
	b.CooldownUntil = time.Time{}
	b.Attempts = 0
	return time.Time{}, false
}

// record folds one actuation outcome into the budget: a converged outcome (ok,
// not_present) clears the item's history, "blocked" starts the escalating long
// back-off at once, and any other failure counts towards the maxAttempts'th,
// which starts the ordinary cooldown.
func (s *budgetState) record(key string, now time.Time, outcome string) {
	if outcome == OutcomeOK || outcome == OutcomeNotPresent {
		delete(s.Items, key)
		return
	}
	b := s.Items[key]
	if b == nil {
		b = &itemBudget{}
		s.Items[key] = b
	}
	b.LastAttempt = now
	if outcome == OutcomeBlocked {
		b.BlockedStreak++
		b.CooldownUntil = now.Add(blockedBackoff(b.BlockedStreak))
		b.Attempts = 0
		return
	}
	b.Attempts++
	if b.Attempts >= maxAttempts {
		b.CooldownUntil = now.Add(cooldown)
		b.Attempts = 0
	}
}

// blockedBackoff is 7 d, 14 d, then 28 d for every further refusal.
func blockedBackoff(streak int) time.Duration {
	d := blockedCooldown
	for i := 1; i < streak && d < blockedCooldownMax; i++ {
		d *= 2
	}
	if d > blockedCooldownMax {
		d = blockedCooldownMax
	}
	return d
}

// budgetStore persists budgetState to <StateDir>/hygiene_budget.json (0600).
type budgetStore struct {
	log  *slog.Logger
	path string
}

func newBudgetStore(log *slog.Logger) *budgetStore {
	return &budgetStore{log: log, path: filepath.Join(config.StateDir(), "hygiene_budget.json")}
}

// load returns the persisted budget; a missing or corrupt file yields an empty
// one (fail-open on bookkeeping, never on safety).
func (s *budgetStore) load() *budgetState {
	st := &budgetState{Items: map[string]*itemBudget{}}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return st
	}
	var on budgetState
	if err := json.Unmarshal(b, &on); err != nil || on.Items == nil {
		return st
	}
	st.Items = on.Items
	return st
}

// save writes the budget back, pruning entries that carry no information.
func (s *budgetStore) save(st *budgetState) {
	for k, b := range st.Items {
		if b == nil || (b.Attempts == 0 && b.CooldownUntil.IsZero() && b.BlockedStreak == 0) {
			delete(st.Items, k)
		}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(s.path, b, 0o600); err != nil {
		s.log.Debug("hygiene budget write failed", "err", err.Error())
	}
}

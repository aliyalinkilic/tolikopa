// Package policy holds the remediation policy the server last handed this agent,
// validates it against the compiled catalog, and persists it across restarts.
//
// AUTHORITY: the server may only SELECT FROM the agent's compiled vocabulary
// (internal/appcatalog). Everything arriving here is therefore filtered before it
// is stored: unknown item_ids are dropped and reported back as "unknown_item",
// over-long lists are refused wholesale. Nothing in this package actuates
// anything — see internal/actuator.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// MaxIDs caps each selection list. A policy that exceeds it is refused entirely:
// a fleet-wide list this long is far more likely to be a bug or a compromised
// server than a deliberate admin action, and the blast radius of guessing wrong
// is every PC in the org.
const MaxIDs = 256

// MaxRevisionLen and MaxIDLen bound the strings the agent persists and echoes
// back. Both match the server's own trim limits, so anything longer could never
// round-trip successfully anyway.
const (
	MaxRevisionLen = 64
	MaxIDLen       = 64
)

// StateFile is the on-disk shape of state/policy.json.
type State struct {
	// Policy is the last ACCEPTED policy (already filtered against the catalog).
	Policy model.RemediationPolicy `json:"policy"`
	// Rejected holds the "kind:item_id" pairs the server sent that this agent's
	// catalog does not contain. They are kept so every hygiene report can tell the
	// server "I do not know these", instead of silently ignoring them once.
	Rejected []string `json:"rejected,omitempty"`
	// AppliedAt is when the policy was accepted and stored (UTC).
	AppliedAt time.Time `json:"applied_at"`
}

// Store persists the accepted policy to state/policy.json (0600).
type Store struct {
	path string

	mu     sync.Mutex
	cur    State
	loaded bool
}

// NewStore returns a Store backed by <StateDir>/policy.json.
func NewStore() *Store { return NewStoreAt(filepath.Join(config.StateDir(), "policy.json")) }

// NewStoreAt returns a Store backed by an explicit path (tests / alternate roots).
func NewStoreAt(path string) *Store { return &Store{path: path} }

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Load reads the persisted state. A missing or corrupt file yields the zero state
// (revision "", nothing selected) — the agent then simply reconciles nothing
// until the server hands it a policy again.
func (s *Store) Load() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.cur
	}
	s.loaded = true
	b, err := os.ReadFile(s.path)
	if err != nil {
		return s.cur
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return s.cur
	}
	s.cur = st
	return s.cur
}

// Save atomically replaces the persisted state.
func (s *Store) Save(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("policy store rename: %w", err)
	}
	s.mu.Lock()
	s.cur, s.loaded = st, true
	s.mu.Unlock()
	return nil
}

// Current returns the in-memory state, loading it on first use.
func (s *Store) Current() State { return s.Load() }

// Validate filters a server-sent policy against the compiled catalog.
//
// It returns the accepted policy and the "kind:item_id" pairs that were dropped
// because this agent's vocabulary does not contain them; the caller reports those
// back with outcome "unknown_item".
//
// accepted.Revision == "" means the policy was refused WHOLESALE and must not be
// stored or applied. That happens when the revision is empty (an unversioned
// directive cannot be acknowledged, so it can never converge) or when a selection
// list exceeds MaxIDs.
func Validate(p *model.RemediationPolicy) (accepted model.RemediationPolicy, rejected []string) {
	if p == nil {
		return model.RemediationPolicy{}, nil
	}
	// The revision is echoed back on EVERY subsequent ingest and persisted to disk.
	// A hostile or broken server could otherwise hand us a ~1 MB revision string
	// (the response body cap): the server would trim it on receipt, so it would
	// never match, never converge, and the agent would upload it forever.
	if rev := strings.TrimSpace(p.Revision); rev == "" || len(rev) > MaxRevisionLen {
		return model.RemediationPolicy{}, nil
	}
	if len(p.Apps) > MaxIDs || len(p.Startup) > MaxIDs {
		return model.RemediationPolicy{}, nil
	}

	accepted = model.RemediationPolicy{
		Revision: p.Revision,
		Enabled:  p.Enabled,
		DryRun:   p.DryRun,
	}
	accepted.Apps, rejected = filter(appcatalog.KindApp, p.Apps, rejected)
	accepted.Startup, rejected = filter(appcatalog.KindStartup, p.Startup, rejected)
	sort.Strings(rejected)
	return accepted, rejected
}

// filter keeps the ids the catalog knows, appending the rest to rejected as
// "kind:item_id". Duplicates within one list are collapsed.
func filter(kind string, ids []string, rejected []string) ([]string, []string) {
	seen := make(map[string]bool, len(ids))
	var out []string
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := appcatalog.Lookup(kind, id); ok {
			out = append(out, id)
			continue
		}
		// An over-long id is dropped silently rather than reported: rejected ids are
		// re-sent as unknown_item attempts on every hygiene pass, so echoing junk
		// back would make a hostile policy a permanent upload amplifier.
		if len(id) > MaxIDLen {
			continue
		}
		rejected = append(rejected, kind+":"+id)
	}
	return out, rejected
}

// SplitRejected parses a "kind:item_id" entry produced by Validate.
func SplitRejected(s string) (kind, itemID string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

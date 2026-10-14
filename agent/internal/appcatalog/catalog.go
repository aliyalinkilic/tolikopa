// Package appcatalog is the agent's COMPILED, CLOSED vocabulary of things the
// panel may ask it to remove or disable.
//
// This package is the enforcement point for the whole endpoint-hygiene feature.
// The server can only ever SELECT FROM this vocabulary: any item_id the agent does
// not find here is refused locally and reported back as outcome "unknown_item".
// Extending the vocabulary therefore requires shipping a new tolikopa-agent.exe through the
// audited GPO path (deploy/gpo/bootstrap.ps1), not a database write. That is what
// keeps this a desired-state reconciler rather than a remote command channel —
// see CLAUDE.md §Safety.
//
// catalog.json is the CANONICAL copy. server/internal/appcatalog/catalog.json is a
// byte-identical generated copy (server/tools/synccatalog). `bash deploy/check.sh`
// diffs the two — run it before merging. Never hand-edit the server copy.
package appcatalog

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

// Kinds of remediable item. Startup entries are reversible (disable, never
// delete); apps are not, which is why they carry separate kill switches.
const (
	KindApp     = "app"
	KindStartup = "startup"
)

// Removal scope for an app. "both" removes for existing users AND deprovisions so
// that newly created profiles do not get the package back.
const (
	ScopeUser        = "user"
	ScopeProvisioned = "provisioned"
	ScopeBoth        = "both"
)

// Risk tiers drive how the panel presents an item; they do not gate anything on
// their own. "advanced" items are never safe_default.
const (
	RiskSafe     = "safe"
	RiskReview   = "review"
	RiskAdvanced = "advanced"
)

// How Item.Family is compared against an observed name. EXACT is the default and
// the safe one: short families like "Skype" or "OneDrive" would prefix-match Skype
// for Business and OneDriveSetup. Prefix is opt-in, for entries that genuinely
// carry a machine-specific suffix.
const (
	MatchExact  = "exact"
	MatchPrefix = "prefix"
)

// Item is one entry of the closed vocabulary.
type Item struct {
	Kind   string `json:"kind"`
	ItemID string `json:"item_id"`
	// Family is the Windows PackageFamilyName for apps, or the Run-value NAME for
	// startup entries. For startup entries this is deliberately the value name and
	// never the value DATA, which is a file path — a forbidden data class.
	Family string `json:"family"`
	// Match is how Family is compared: "" / "exact" (default) or "prefix".
	// Startup entries only; apps always compare the family exactly.
	Match       string `json:"match,omitempty"`
	DisplayName string `json:"display_name"` // Turkish, admin-facing
	Category    string `json:"category"`
	RiskTier    string `json:"risk_tier"`
	Scope       string `json:"scope,omitempty"`
	SafeDefault bool   `json:"safe_default"`
	// ProcessNames are normalized basenames (lowercase, no ".exe") used to tie an
	// item back to the process/CPU telemetry the fleet already collects.
	ProcessNames []string `json:"process_names,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

type file struct {
	Schema int    `json:"schema"`
	Items  []Item `json:"items"`
}

var (
	once    sync.Once
	items   []Item
	byKey   map[string]Item
	version string
	loadErr error
)

func load() {
	var f file
	if err := json.Unmarshal(catalogJSON, &f); err != nil {
		loadErr = fmt.Errorf("appcatalog: parse catalog.json: %w", err)
		return
	}
	if f.Schema != 1 {
		loadErr = fmt.Errorf("appcatalog: unsupported schema %d", f.Schema)
		return
	}
	byKey = make(map[string]Item, len(f.Items))
	for _, it := range f.Items {
		if err := validate(it); err != nil {
			loadErr = err
			return
		}
		k := key(it.Kind, it.ItemID)
		if _, dup := byKey[k]; dup {
			loadErr = fmt.Errorf("appcatalog: duplicate item %q", k)
			return
		}
		byKey[k] = it
	}
	items = f.Items
	version = computeVersion(f.Items)
}

func validate(it Item) error {
	if it.Kind != KindApp && it.Kind != KindStartup {
		return fmt.Errorf("appcatalog: item %q: bad kind %q", it.ItemID, it.Kind)
	}
	if !validID(it.ItemID) {
		return fmt.Errorf("appcatalog: bad item_id %q (want [a-z0-9][a-z0-9._-]{0,63})", it.ItemID)
	}
	if it.Family == "" || it.DisplayName == "" {
		return fmt.Errorf("appcatalog: item %q: family and display_name are required", it.ItemID)
	}
	// The forbidden table is the last line of defence; a catalog that trips it is a
	// build-time bug, not a runtime condition. Fail loudly rather than ship it.
	if it.Kind == KindApp && IsForbiddenFamily(it.Family) {
		return fmt.Errorf("appcatalog: item %q targets protected package %q", it.ItemID, it.Family)
	}
	if it.SafeDefault && it.RiskTier != RiskSafe {
		return fmt.Errorf("appcatalog: item %q: safe_default requires risk_tier=safe", it.ItemID)
	}
	switch it.Match {
	case "", MatchExact, MatchPrefix:
	default:
		return fmt.Errorf("appcatalog: item %q: bad match %q (want exact|prefix)", it.ItemID, it.Match)
	}
	// Prefix matching on an app family would let one catalog entry act on unrelated
	// packages; it exists only for startup value names with machine-specific suffixes.
	if it.Match == MatchPrefix && it.Kind != KindStartup {
		return fmt.Errorf("appcatalog: item %q: match=prefix is startup-only", it.ItemID)
	}
	return nil
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

func key(kind, id string) string { return kind + ":" + id }

// computeVersion hashes ONLY the enforcement-relevant vocabulary (kind:item_id),
// never display names, notes, categories or risk tiers. A Turkish label edit
// therefore does not invalidate deployed agents; adding or retiring an item does.
// docs/AGENT.md specifies this function; server/internal/appcatalog must match it
// byte for byte, and both sides cover it with the same golden vector.
func computeVersion(list []Item) string {
	keys := make([]string, 0, len(list))
	for _, it := range list {
		keys = append(keys, key(it.Kind, it.ItemID))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return "v1-" + hex.EncodeToString(sum[:])[:12]
}

func ensure() {
	once.Do(load)
	if loadErr != nil {
		// The catalog is embedded at build time; a failure here means the binary
		// itself is malformed and no amount of runtime handling helps.
		panic(loadErr)
	}
}

// Version returns the vocabulary hash the agent reports on every ingest so the
// server can flag catalog drift.
func Version() string { ensure(); return version }

// All returns every catalog item. The slice is shared; callers must not mutate it.
func All() []Item { ensure(); return items }

// Lookup resolves a (kind, item_id) pair. ok=false means the server referenced
// something outside this agent's vocabulary — refuse it and report unknown_item.
func Lookup(kind, id string) (Item, bool) {
	ensure()
	it, ok := byKey[key(kind, id)]
	return it, ok
}

// Kind returns every item of one kind, in catalog order.
func Kind(kind string) []Item {
	ensure()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// RawJSON exposes the embedded bytes so the sync tool can copy them verbatim.
func RawJSON() []byte { return catalogJSON }

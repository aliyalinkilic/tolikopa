// Package appcatalog is the SERVER's read-only mirror of the agent's compiled,
// closed remediation vocabulary.
//
// AUTHORITY: the agent's copy (agent/internal/appcatalog) is the enforcement
// point. The server may only ever SELECT FROM this vocabulary — it validates the
// item_ids an admin selects and it labels what agents report, but an item_id the
// agent does not carry is refused on the endpoint and reported back as outcome
// "unknown_item". Extending the vocabulary therefore requires shipping a new
// tolikopa-agent.exe through the audited GPO path, never a database write. See
// CLAUDE.md §Safety.
//
// catalog.json here is a GENERATED, byte-identical copy of
// agent/internal/appcatalog/catalog.json, produced by server/tools/synccatalog.
// Go's embed cannot cross module boundaries ("..' is illegal in an embed pattern),
// so a physical copy is unavoidable — but it is generated and diff-checked, not
// hand-synced. Never hand-edit this file:
//
//	docker run --rm -v "$PWD:/repo" -w /repo/server golang:1.26 go run ./tools/synccatalog
//	git diff --exit-code server/internal/appcatalog/catalog.json   # CI gate
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

// How Item.Family is compared against an observed name. EXACT is the default and
// the safe one: short families like "Skype" or "OneDrive" would prefix-match Skype
// for Business and OneDriveSetup. Prefix is opt-in, for entries that genuinely
// carry a machine-specific suffix.
const (
	MatchExact  = "exact"
	MatchPrefix = "prefix"
)

// Risk tiers drive how the panel presents an item; they do not gate anything on
// their own. "advanced" items are never safe_default.
const (
	RiskSafe     = "safe"
	RiskReview   = "review"
	RiskAdvanced = "advanced"
)

// Item is one entry of the closed vocabulary. Field names and JSON tags are
// identical to the agent's Item — the JSON file is shared byte for byte.
type Item struct {
	Kind   string `json:"kind"`
	ItemID string `json:"item_id"`
	// Family is the Windows PackageFamilyName for apps, or the Run-value NAME for
	// startup entries. For startup entries this is deliberately the value name and
	// never the value DATA, which is a file path — a forbidden data class.
	Family      string `json:"family"`
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
	list, ver, err := Parse(catalogJSON)
	if err != nil {
		loadErr = err
		return
	}
	byKey = make(map[string]Item, len(list))
	for _, it := range list {
		byKey[key(it.Kind, it.ItemID)] = it
	}
	items = list
	version = ver
}

// Parse decodes a catalog document and returns its items plus the vocabulary
// version, without touching the embedded copy. server/tools/synccatalog uses it
// to report what it just wrote so the two implementations never diverge.
func Parse(data []byte) ([]Item, string, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, "", fmt.Errorf("appcatalog: parse catalog.json: %w", err)
	}
	if f.Schema != 1 {
		return nil, "", fmt.Errorf("appcatalog: unsupported schema %d", f.Schema)
	}
	seen := make(map[string]struct{}, len(f.Items))
	for _, it := range f.Items {
		if err := validate(it); err != nil {
			return nil, "", err
		}
		k := key(it.Kind, it.ItemID)
		if _, dup := seen[k]; dup {
			return nil, "", fmt.Errorf("appcatalog: duplicate item %q", k)
		}
		seen[k] = struct{}{}
	}
	return f.Items, computeVersion(f.Items), nil
}

// validate mirrors the agent's structural checks, INCLUDING the forbidden-package
// table (forbidden.go, a hand-kept copy). The server never actuates, so this is
// not an enforcement guard — it stops a bad catalog from being mirrored into
// remediation_item and offered to an admin in the panel while every agent refuses
// to load it. Failing at server startup is a much louder signal than a fleet of
// panicking agents.
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
	// Defence in depth: the agent refuses protected packages anyway, but without
	// this a catalog naming one would be mirrored into remediation_item and offered
	// to the admin in the panel while every agent panicked on load.
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
//
// This MUST stay byte-for-byte identical to the agent's implementation: the drift
// handshake compares the agent's reported version against Version() here, and any
// disagreement would silently mark the entire fleet stale. Both sides pin it with
// the same golden vector (TestComputeVersionGoldenVector).
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

// Version returns the vocabulary hash used for agent drift detection.
func Version() string { ensure(); return version }

// All returns every catalog item in file order. The slice is shared; callers must
// not mutate it.
func All() []Item { ensure(); return items }

// Lookup resolves a (kind, item_id) pair. ok=false means the id is outside the
// vocabulary — reject it on admin writes, and store it as free text (never a FK)
// when an agent reports it.
func Lookup(kind, id string) (Item, bool) {
	ensure()
	it, ok := byKey[key(kind, id)]
	return it, ok
}

//go:build windows

package collector

import (
	"log/slog"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// Registry locations of logon (Run) entries, read by a LocalSystem service for
// the machine and for every LOADED user hive under HKEY_USERS.
//
// PRIVACY — the single most important constraint in this file: we read the value
// NAMES only (registry.ReadValueNames). The value DATA of a Run entry is a
// command line / file path, which is a forbidden data class; it is never read,
// never logged and has no field in model.RemediationItem.
const (
	runPath      = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	runOncePath  = `SOFTWARE\Microsoft\Windows\CurrentVersion\RunOnce`
	run32Path    = `SOFTWARE\Wow6432Node\Microsoft\Windows\CurrentVersion\Run`
	approvedBase = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved`
)

// StartupApproved subkeys. "StartupFolder" governs Startup-folder shortcuts,
// which this collector does not enumerate (we only look at Run values), but the
// name is kept here so the mapping is documented in one place.
const (
	approvedRun    = "Run"
	approvedRun32  = "Run32"
	approvedFolder = "StartupFolder"
)

type winStartup struct {
	log *slog.Logger

	mu      sync.Mutex
	targets []StartupTarget
}

// NewStartupCollector returns the Windows Run/RunOnce startup collector.
func NewStartupCollector(log *slog.Logger) StartupCollector {
	return &winStartup{log: log}
}

// runSource is one Run-key location to enumerate.
type runSource struct {
	root     registry.Key
	path     string // relative to root
	userSID  string // "" = machine (HKLM)
	approved string // StartupApproved subkey governing it; "" = none (RunOnce)
}

// Collect reports the CATALOG logon entries present on this machine and whether
// they currently run at logon. Entries outside the compiled catalog are ignored
// entirely — this is not a startup inventory.
func (w *winStartup) Collect() []model.RemediationItem {
	catalog := appcatalog.Kind(appcatalog.KindStartup)
	if len(catalog) == 0 {
		return nil
	}

	var targets []StartupTarget
	for _, src := range w.sources() {
		targets = append(targets, w.scanSource(src, catalog)...)
	}

	// Aggregate per catalog item: an item counts as enabled when ANY of its
	// observed entries still runs at logon.
	type agg struct {
		enabled bool
		order   int
	}
	seen := map[string]*agg{}
	var order []string
	for _, t := range targets {
		a := seen[t.ItemID]
		if a == nil {
			a = &agg{order: len(order)}
			seen[t.ItemID] = a
			order = append(order, t.ItemID)
		}
		if t.Enabled {
			a.enabled = true
		}
	}

	out := make([]model.RemediationItem, 0, len(order))
	for _, id := range order {
		enabled := seen[id].enabled
		out = append(out, model.RemediationItem{
			Kind:    appcatalog.KindStartup,
			ItemID:  id,
			Present: true,
			Enabled: &enabled,
		})
	}

	w.mu.Lock()
	w.targets = targets
	w.mu.Unlock()

	w.log.Debug("startup inventory", "catalog_matches", len(out))
	return out
}

// Targets returns the exact registry values observed by the last Collect.
// Actuator-only detail (it carries a SID); it never reaches model/the wire.
func (w *winStartup) Targets() []StartupTarget {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]StartupTarget, len(w.targets))
	copy(out, w.targets)
	return out
}

// sources lists the machine Run keys plus one set per loaded user hive.
func (w *winStartup) sources() []runSource {
	out := []runSource{
		{root: registry.LOCAL_MACHINE, path: runPath, approved: approvedRun},
		{root: registry.LOCAL_MACHINE, path: run32Path, approved: approvedRun32},
		// RunOnce has no StartupApproved counterpart: Windows deletes the value
		// after it runs, so there is nothing to "disable".
		{root: registry.LOCAL_MACHINE, path: runOncePath},
	}
	for _, sid := range w.loadedUserHives() {
		out = append(out,
			runSource{root: registry.USERS, path: sid + `\` + runPath, userSID: sid, approved: approvedRun},
			runSource{root: registry.USERS, path: sid + `\` + run32Path, userSID: sid, approved: approvedRun32},
			runSource{root: registry.USERS, path: sid + `\` + runOncePath, userSID: sid},
		)
	}
	return out
}

// loadedUserHives returns the SIDs of the user hives currently loaded under
// HKEY_USERS. Hives of logged-off users are simply not there — hygiene state for
// them is picked up the next time they log on. Class hives (…_Classes) and the
// three service accounts are skipped.
func (w *winStartup) loadedUserHives() []string {
	k, err := registry.OpenKey(registry.USERS, "", registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		w.log.Debug("HKU enumerate failed", "err", err.Error())
		return nil
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		u := strings.ToUpper(n)
		if !strings.HasPrefix(u, "S-1-") || strings.HasSuffix(u, "_CLASSES") {
			continue
		}
		switch u {
		case "S-1-5-18", "S-1-5-19", "S-1-5-20":
			continue
		}
		out = append(out, n)
	}
	return out
}

// scanSource enumerates one Run key's value NAMES and keeps the catalog matches.
func (w *winStartup) scanSource(src runSource, catalog []appcatalog.Item) []StartupTarget {
	k, err := registry.OpenKey(src.root, src.path, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil // key absent (normal) or not readable
	}
	defer k.Close()

	names, err := k.ReadValueNames(-1) // NAMES ONLY — never GetStringValue here
	if err != nil {
		return nil
	}

	approved := w.approvedStates(src)

	var out []StartupTarget
	for _, name := range names {
		it, ok := matchStartupItem(catalog, name)
		if !ok {
			continue // not in the vocabulary: invisible to Tolikopa
		}
		enabled := true
		if blob, present := approved[strings.ToLower(name)]; present {
			e, known := startupApprovedEnabled(blob)
			if !known {
				w.log.Debug("unknown StartupApproved blob, assuming enabled",
					"item_id", it.ItemID, "len", len(blob))
			}
			enabled = e
		}
		out = append(out, StartupTarget{
			ItemID:    it.ItemID,
			ValueName: name,
			UserSID:   src.userSID,
			Approved:  src.approved,
			Enabled:   enabled,
		})
	}
	return out
}

// approvedStates reads the StartupApproved blobs governing one Run key, keyed by
// lowercase value name. A missing key or value means "never toggled" = enabled.
func (w *winStartup) approvedStates(src runSource) map[string][]byte {
	if src.approved == "" {
		return nil
	}
	path := approvedBase + `\` + src.approved
	if src.userSID != "" {
		path = src.userSID + `\` + path
	}
	k, err := registry.OpenKey(src.root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil
	}
	out := make(map[string][]byte, len(names))
	for _, n := range names {
		// The DATA here is a 12-byte state blob (see startupApprovedEnabled), not
		// a path — reading it is within the allowlist.
		if b, _, err := k.GetBinaryValue(n); err == nil {
			out[strings.ToLower(n)] = b
		}
	}
	return out
}

// matchStartupItem and startupApprovedEnabled live in hygiene_match.go so the
// matching and blob-decoding rules can be unit-tested off Windows.

// StartupApprovedPath returns the registry root and path of the StartupApproved
// key that governs t, so the actuator writes state to exactly the key this
// collector reads it from. ok=false when the entry has no StartupApproved
// counterpart (RunOnce), i.e. it cannot be disabled without deleting the value —
// which Tolikopa never does.
func StartupApprovedPath(t StartupTarget) (root registry.Key, path string, ok bool) {
	if t.Approved == "" {
		return 0, "", false
	}
	if t.UserSID == "" {
		return registry.LOCAL_MACHINE, approvedBase + `\` + t.Approved, true
	}
	return registry.USERS, t.UserSID + `\` + approvedBase + `\` + t.Approved, true
}

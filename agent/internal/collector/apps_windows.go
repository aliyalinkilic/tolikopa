//go:build windows

package collector

import (
	"log/slog"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// The agent runs as LocalSystem in session 0, so it has no user's WinRT context
// and Get-AppxPackage-style enumeration is unavailable to it. The machine-wide
// registry view below is what a service can actually read, and it covers EVERY
// user profile plus the provisioned (per-image) store:
//
//	HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Appx\AppxAllUserStore
//	  ├─ S-1-5-21-…            one key per user SID; subkeys are PackageFullNames
//	  ├─ Applications          provisioned packages;   subkeys are PackageFullNames
//	  ├─ InboxApplications     inbox packages;         subkeys are PackageFullNames
//	  ├─ Deprovisioned         subkeys are PackageFAMILYNames already suppressed
//	  └─ EndOfLife, …          ignored
//
// Everything here is opened READ-ONLY (QUERY_VALUE|ENUMERATE_SUB_KEYS) and no
// value DATA is ever read — only key names.
const appxAllUserStore = `SOFTWARE\Microsoft\Windows\CurrentVersion\Appx\AppxAllUserStore`

// WHAT THIS KEY CAN AND CANNOT ANSWER: the subkey names under a user SID look
// exactly like PackageFullNames, but the key is a deployment JOURNAL, not a live
// package list. It accumulates one subkey per version ever deployed (25 Spotify
// versions on the machine this was diagnosed on), keeps them after the package is
// removed, and the version actually registered may be missing from it entirely.
//
// So it is used for ONE thing: "has this catalog family ever been deployed here",
// which is a good enough presence hint for the panel and cheap for a service to
// read. It is NOT used to derive removal targets — those are resolved against the
// deployment API at removal time (actuator/appx_resolve_windows.go). Handing
// these names to the removal worker is what made every removal answer 0x80070490.

type winApps struct {
	log *slog.Logger
}

// NewAppInventoryCollector returns the Windows AppxAllUserStore inventory collector.
func NewAppInventoryCollector(log *slog.Logger) AppInventoryCollector {
	return &winApps{log: log}
}

// storeScan is the raw, lowercase-keyed result of one registry pass.
type storeScan struct {
	user          map[string]bool // family -> deployed for at least one user
	provisioned   map[string]bool // family -> present in the provisioned/inbox store
	deprovisioned map[string]bool // family -> suppressed for new profiles
}

func newStoreScan() *storeScan {
	return &storeScan{
		user:          map[string]bool{},
		provisioned:   map[string]bool{},
		deprovisioned: map[string]bool{},
	}
}

// Collect reports which CATALOG apps are present, and where. Packages outside the
// compiled catalog are counted for nothing and never leave this function: this is
// deliberately not a general software inventory.
func (w *winApps) Collect() []model.RemediationItem {
	scan := w.scan()
	if scan == nil {
		return nil
	}

	var out []model.RemediationItem
	for _, it := range appcatalog.Kind(appcatalog.KindApp) {
		fam := strings.ToLower(it.Family)
		inUser := scan.user[fam]
		// A provisioned package that is already deprovisioned will not come back
		// for new profiles, so it does not count as "provisioned" any more.
		inProv := scan.provisioned[fam] && !scan.deprovisioned[fam]
		if !inUser && !inProv {
			continue // absent: nothing to report, nothing to act on
		}

		scope := appcatalog.ScopeProvisioned
		switch {
		case inUser && inProv:
			scope = appcatalog.ScopeBoth
		case inUser:
			scope = appcatalog.ScopeUser
		}
		out = append(out, model.RemediationItem{
			Kind:    appcatalog.KindApp,
			ItemID:  it.ItemID,
			Present: true,
			Scope:   scope,
		})
	}

	w.log.Debug("appx inventory", "catalog_matches", len(out))
	return out
}

// scan walks AppxAllUserStore once. Failures degrade to "nothing found" — never
// to a guess, and never to an error that could stall the agent.
func (w *winApps) scan() *storeScan {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, appxAllUserStore,
		registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		w.log.Debug("appx store unavailable", "err", err.Error())
		return nil
	}
	defer root.Close()

	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		w.log.Debug("appx store enumerate failed", "err", err.Error())
		return nil
	}

	scan := newStoreScan()
	for _, name := range names {
		switch {
		case strings.EqualFold(name, "Deprovisioned"):
			for _, fam := range w.subKeyNames(name) {
				scan.deprovisioned[strings.ToLower(fam)] = true
			}
		case strings.EqualFold(name, "Applications"), strings.EqualFold(name, "InboxApplications"):
			w.absorb(scan.provisioned, name)
		case isUserSID(name):
			w.absorb(scan.user, name)
		default:
			// EndOfLife, Volumes, PackageRepository, … : not presence information.
		}
	}
	return scan
}

// absorb records the FAMILY of every entry under one subkey into the given
// bucket. The full names themselves are deliberately discarded — see the note at
// the top of this file.
func (w *winApps) absorb(bucket map[string]bool, sub string) {
	for _, full := range w.subKeyNames(sub) {
		if fam := familyFromFullName(full); fam != "" {
			bucket[strings.ToLower(fam)] = true
		}
	}
}

// subKeyNames opens one subkey of the store read-only and returns its child names.
func (w *winApps) subKeyNames(sub string) []string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, appxAllUserStore+`\`+sub,
		registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	return names
}

// familyFromFullName and isUserSID live in hygiene_match.go so the matching rules
// can be unit-tested off Windows.

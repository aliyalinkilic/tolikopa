package collector

import (
	"strings"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
)

// Pure matching logic for the hygiene collectors. It lives outside the
// //go:build windows files on purpose: this is where a mistake would either leak
// a non-catalog package into the report or silently match nothing, and it is the
// part that can be unit-tested on any platform.

// familyFromFullName derives a PackageFamilyName from a PackageFullName.
//
//	Name_Version_Architecture_ResourceId_PublisherId  ->  Name_PublisherId
//	Microsoft.BingWeather_4.53.5_x64__8wekyb3d8bbwe   ->  Microsoft.BingWeather_8wekyb3d8bbwe
//
// Package names may contain dots and dashes but not underscores, so splitting on
// "_" and taking the first and last field is exact. Anything with fewer than
// three fields is not a full name and is ignored (fail closed: an unparsed name
// matches no catalog item and can therefore never be acted on).
func familyFromFullName(full string) string {
	parts := strings.Split(strings.TrimSpace(full), "_")
	if len(parts) < 3 {
		return ""
	}
	name, publisher := parts[0], parts[len(parts)-1]
	if name == "" || publisher == "" {
		return ""
	}
	return name + "_" + publisher
}

// isUserSID reports whether an AppxAllUserStore / HKEY_USERS subkey names a user
// profile. The three well-known service accounts and the class hives are skipped.
// The SID itself is used only to address the registry and is never emitted.
func isUserSID(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(u, "S-1-") || strings.HasSuffix(u, "_CLASSES") {
		return false
	}
	switch u {
	case "S-1-5-18", "S-1-5-19", "S-1-5-20":
		return false
	}
	return true
}

// matchStartupItem resolves a Run value NAME to a catalog item, case-insensitively.
//
// Matching is EXACT by default. Prefix matching is opt-in per item
// (Item.Match == appcatalog.MatchPrefix) and exists only for entries that carry a
// machine-specific suffix, e.g. MicrosoftEdgeAutoLaunch_8f3a…
//
// Exact-by-default is a safety property, not a style choice: several catalog
// families are short words that prefix-match unrelated business software —
// "Skype" would catch Skype for Business, "OneDrive" would catch OneDriveSetup and
// the standalone updater, "Zoom" would catch ZoomOutlookPlugin. Disabling one of
// those because an admin ticked the consumer app is exactly the kind of silent
// collateral damage this feature must not cause. A family that matches nothing is
// harmless; a family that matches too much is not.
//
// The first catalog entry that matches wins (catalog order is stable).
func matchStartupItem(catalog []appcatalog.Item, valueName string) (appcatalog.Item, bool) {
	n := strings.ToLower(strings.TrimSpace(valueName))
	if n == "" {
		return appcatalog.Item{}, false
	}
	for _, it := range catalog {
		fam := strings.ToLower(strings.TrimSpace(it.Family))
		if fam == "" {
			continue
		}
		if it.Match == appcatalog.MatchPrefix {
			if strings.HasPrefix(n, fam) {
				return it, true
			}
			continue
		}
		if n == fam {
			return it, true
		}
	}
	return appcatalog.Item{}, false
}

// startupApprovedEnabled decodes a StartupApproved value blob.
//
// Layout (undocumented by Microsoft, stable since Windows 8 and what Task Manager
// and the Settings "Startup apps" page write):
//
//	byte  0     state byte
//	bytes 1-3   zero
//	bytes 4-11  FILETIME of the last state change (all-zero when never toggled)
//
// Observed state bytes: 0x02 and 0x06 = enabled, 0x03 and 0x07 = disabled. The
// low bit appears to be the "disabled" flag, but we deliberately do NOT
// extrapolate: an unrecognised blob is reported as ENABLED with known=false so
// the caller can log it, rather than guessing "disabled" and silently skipping an
// entry an admin asked us to handle.
func startupApprovedEnabled(b []byte) (enabled, known bool) {
	if len(b) == 0 {
		return true, false
	}
	switch b[0] {
	case 0x02, 0x06:
		return true, true
	case 0x03, 0x07:
		return false, true
	default:
		return true, false
	}
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

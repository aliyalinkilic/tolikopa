package collector

import (
	"testing"

	"github.com/aliyalinkilic/tolikopa/agent/internal/appcatalog"
)

func TestFamilyFromFullName(t *testing.T) {
	cases := map[string]string{
		"Microsoft.BingWeather_4.53.51361.0_x64__8wekyb3d8bbwe":                      "Microsoft.BingWeather_8wekyb3d8bbwe",
		"Microsoft.BingWeather_4.53.51361.0_neutral_split.scale-100_8wekyb3d8bbwe":   "Microsoft.BingWeather_8wekyb3d8bbwe",
		"Clipchamp.Clipchamp_2.8.0.0_neutral__yxz26nhyzhsrt":                         "Clipchamp.Clipchamp_yxz26nhyzhsrt",
		"microsoft.windowscommunicationsapps_16005.14326.21470.0_x64__8wekyb3d8bbwe": "microsoft.windowscommunicationsapps_8wekyb3d8bbwe",
		// Not full names: must fail closed (no family => matches nothing).
		"Microsoft.BingWeather_8wekyb3d8bbwe": "",
		"NotAPackage":                         "",
		"":                                    "",
		"_1_x64__":                            "",
	}
	for in, want := range cases {
		if got := familyFromFullName(in); got != want {
			t.Errorf("familyFromFullName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFamilyFromFullNameMatchesCatalog is the property that makes the inventory
// work at all: a real PackageFullName must reduce to the exact `family` string
// the catalog carries, case-insensitively.
func TestFamilyFromFullNameMatchesCatalog(t *testing.T) {
	full := "Microsoft.MicrosoftSolitaireCollection_4.19.1.0_x64__8wekyb3d8bbwe"
	item, ok := appcatalog.Lookup(appcatalog.KindApp, "solitaire")
	if !ok {
		t.Fatal("catalog lost the solitaire item")
	}
	if got := familyFromFullName(full); !equalFold(got, item.Family) {
		t.Errorf("derived family %q does not match catalog family %q", got, item.Family)
	}
}

func equalFold(a, b string) bool { return len(a) == len(b) && containsFold([]string{a}, b) }

func TestIsUserSID(t *testing.T) {
	yes := []string{"S-1-5-21-1234567890-1234567890-1234567890-1001", "s-1-12-1-11-22-33-44"}
	no := []string{
		"S-1-5-18", "S-1-5-19", "S-1-5-20",
		"S-1-5-21-1-2-3-1001_Classes", ".DEFAULT", "Applications", "Deprovisioned", "",
	}
	for _, s := range yes {
		if !isUserSID(s) {
			t.Errorf("isUserSID(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isUserSID(s) {
			t.Errorf("isUserSID(%q) = true, want false", s)
		}
	}
}

func TestMatchStartupItemPrefixRule(t *testing.T) {
	catalog := appcatalog.Kind(appcatalog.KindStartup)

	// The Edge entry carries a machine-specific suffix; the catalog family is a
	// prefix of it. This is the case the prefix rule exists for.
	if it, ok := matchStartupItem(catalog, "MicrosoftEdgeAutoLaunch_8F3A1C2D4E5F6071"); !ok || it.ItemID != "startup.edge" {
		t.Errorf("edge autolaunch: got %q/%v, want startup.edge", it.ItemID, ok)
	}
	// Case-insensitive.
	if it, ok := matchStartupItem(catalog, "spotify"); !ok || it.ItemID != "startup.spotify" {
		t.Errorf("lowercase spotify: got %q/%v", it.ItemID, ok)
	}
	// Anything outside the vocabulary is invisible — this is what keeps the
	// report from becoming a startup inventory.
	for _, name := range []string{"", "   ", "MuhasebeYedekleme", "SecurityHealth", "RTHDVCPL"} {
		if it, ok := matchStartupItem(catalog, name); ok {
			t.Errorf("matchStartupItem(%q) matched %q, want no match", name, it.ItemID)
		}
	}
}

// TestMatchStartupItemDoesNotOverMatch is the regression guard for the collateral
// damage prefix-by-default would cause: several catalog families are short words
// that are legitimate prefixes of unrelated business software. Disabling Skype for
// Business because an admin ticked consumer Skype is not an acceptable failure.
func TestMatchStartupItemDoesNotOverMatch(t *testing.T) {
	catalog := appcatalog.Kind(appcatalog.KindStartup)

	overMatches := []string{
		"Skype for Business",           // vs family "Skype"
		"SkypeForBusinessLync",         //
		"OneDriveSetup",                // vs family "OneDrive"
		"OneDrive Standalone Updater",  //
		"ZoomOutlookPlugin",            // vs family "Zoom"
		"ZoomVDIUniversalPlugin",       //
		"SteamCloudBackupHelper",       // vs family "Steam"
		"DiscordCanaryDevBuild",        // vs family "Discord"
		"SpotifyWebHelperLegacy",       // vs family "Spotify"
	}
	for _, name := range overMatches {
		if it, ok := matchStartupItem(catalog, name); ok {
			t.Errorf("matchStartupItem(%q) matched %q — exact matching is required for that family", name, it.ItemID)
		}
	}

	// The genuinely exact names must still match.
	exact := map[string]string{
		"Skype":                   "startup.skype",
		"OneDrive":                "startup.onedrive",
		"Zoom":                    "startup.zoom",
		"Steam":                   "startup.steam",
		"com.squirrel.Teams.Teams": "startup.teams",
	}
	for name, want := range exact {
		if it, ok := matchStartupItem(catalog, name); !ok || it.ItemID != want {
			t.Errorf("matchStartupItem(%q) = %q/%v, want %q", name, it.ItemID, ok, want)
		}
	}

	// Adobe's real Run values carry a version suffix, so those two are opt-in prefix.
	for name, want := range map[string]string{
		"AdobeAAMUpdater-1.0": "startup.adobe_updater",
		"AdobeGCInvoker-1.0":  "startup.adobe_gc",
	} {
		if it, ok := matchStartupItem(catalog, name); !ok || it.ItemID != want {
			t.Errorf("matchStartupItem(%q) = %q/%v, want %q", name, it.ItemID, ok, want)
		}
	}
}

func TestStartupApprovedEnabled(t *testing.T) {
	cases := []struct {
		name           string
		blob           []byte
		enabled, known bool
	}{
		{"enabled 0x02", []byte{0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, true, true},
		{"enabled 0x06", []byte{0x06, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}, true, true},
		{"disabled 0x03", []byte{0x03, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}, false, true},
		{"disabled 0x07", []byte{0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, true},
		// Unknown variants must NOT be guessed as disabled.
		{"unknown 0x0b", []byte{0x0b, 0, 0, 0}, true, false},
		{"empty", nil, true, false},
	}
	for _, c := range cases {
		enabled, known := startupApprovedEnabled(c.blob)
		if enabled != c.enabled || known != c.known {
			t.Errorf("%s: got (%v,%v), want (%v,%v)", c.name, enabled, known, c.enabled, c.known)
		}
	}
}

package appcatalog

import "testing"

// TestCatalogLoads exercises every validation rule in load(): bad kinds, malformed
// ids, duplicates, forbidden targets and safe_default/risk_tier disagreement all
// panic here rather than shipping.
func TestCatalogLoads(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("catalog is empty")
	}
	if Version() == "" {
		t.Fatal("catalog version is empty")
	}
	if len(Kind(KindApp)) == 0 || len(Kind(KindStartup)) == 0 {
		t.Fatal("expected both app and startup items")
	}
}

// TestNoCatalogItemIsForbidden is the invariant that matters most: the vocabulary
// must never name a package whose removal breaks Windows. load() already enforces
// it, but assert it explicitly so the failure message points at the real problem.
func TestNoCatalogItemIsForbidden(t *testing.T) {
	for _, it := range Kind(KindApp) {
		if IsForbiddenFamily(it.Family) {
			t.Errorf("catalog item %q targets protected package %q", it.ItemID, it.Family)
		}
	}
}

func TestLookupRejectsUnknownItems(t *testing.T) {
	if _, ok := Lookup(KindApp, "weather"); !ok {
		t.Error("expected to find the weather item")
	}
	// The server can only select from the vocabulary; anything else must be refused
	// locally and reported as unknown_item.
	for _, id := range []string{"", "does-not-exist", "../etc/passwd", "WEATHER"} {
		if _, ok := Lookup(KindApp, id); ok {
			t.Errorf("Lookup(app, %q) unexpectedly succeeded", id)
		}
	}
	if _, ok := Lookup("bogus-kind", "weather"); ok {
		t.Error("Lookup with an unknown kind unexpectedly succeeded")
	}
}

func TestIsForbiddenFamily(t *testing.T) {
	forbidden := []string{
		"Microsoft.WindowsStore_8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_8wekyb3d8bbwe",
		"Microsoft.VCLibs.140.00_8wekyb3d8bbwe",
		"Microsoft.UI.Xaml.2.8_8wekyb3d8bbwe",
		"Microsoft.WindowsAppRuntime.1.5_8wekyb3d8bbwe",
		"Microsoft.AAD.BrokerPlugin_cw5n1h2txyewy",
		"Microsoft.Windows.ShellExperienceHost_cw5n1h2txyewy",
		"Microsoft.SecHealthUI_8wekyb3d8bbwe",
		"Microsoft.MicrosoftEdge.Stable_8wekyb3d8bbwe",
		"Microsoft.Win32WebViewHost_cw5n1h2txyewy",
		"microsoft.windowsstore_8wekyb3d8bbwe", // case-insensitive
		"  Microsoft.WindowsStore_8wekyb3d8bbwe  ",
		"", // empty must fail closed
	}
	for _, f := range forbidden {
		if !IsForbiddenFamily(f) {
			t.Errorf("IsForbiddenFamily(%q) = false, want true", f)
		}
	}

	allowed := []string{
		"Microsoft.BingWeather_8wekyb3d8bbwe",
		"Microsoft.MicrosoftSolitaireCollection_8wekyb3d8bbwe",
		"Clipchamp.Clipchamp_yxz26nhyzhsrt",
		"SpotifyAB.SpotifyMusic_zpdnekdrzrea0",
	}
	for _, f := range allowed {
		if IsForbiddenFamily(f) {
			t.Errorf("IsForbiddenFamily(%q) = true, want false", f)
		}
	}
}

// TestComputeVersionGoldenVector pins the hash ALGORITHM, not the current catalog
// contents (which change legitimately). server/internal/appcatalog carries the
// identical test with the identical expected value — if the two ever disagree the
// drift-detection handshake is broken and every agent would look out of date.
func TestComputeVersionGoldenVector(t *testing.T) {
	in := []Item{
		{Kind: KindStartup, ItemID: "startup.spotify", DisplayName: "değişebilir"},
		{Kind: KindApp, ItemID: "weather", DisplayName: "değişebilir"},
		{Kind: KindApp, ItemID: "news", Notes: "değişebilir"},
	}
	const want = "v1-7cee10fe9ffd"
	if got := computeVersion(in); got != want {
		t.Errorf("computeVersion = %q, want %q", got, want)
	}
}

// TestComputeVersionIgnoresCosmetics is the property the golden vector protects:
// editing Turkish copy must not invalidate every deployed agent, but changing the
// vocabulary must.
func TestComputeVersionIgnoresCosmetics(t *testing.T) {
	base := []Item{{Kind: KindApp, ItemID: "weather", DisplayName: "Hava Durumu", RiskTier: RiskSafe}}
	renamed := []Item{{Kind: KindApp, ItemID: "weather", DisplayName: "Hava Durumu (MSN)", RiskTier: RiskReview, Notes: "yeni not"}}
	if computeVersion(base) != computeVersion(renamed) {
		t.Error("cosmetic edits changed the catalog version")
	}

	added := append(append([]Item{}, base...), Item{Kind: KindApp, ItemID: "news"})
	if computeVersion(base) == computeVersion(added) {
		t.Error("adding an item did not change the catalog version")
	}

	// Order in the JSON file must not matter — both sides sort before hashing.
	shuffled := []Item{{Kind: KindApp, ItemID: "news"}, {Kind: KindApp, ItemID: "weather", DisplayName: "Hava Durumu", RiskTier: RiskSafe}}
	if computeVersion(added) != computeVersion(shuffled) {
		t.Error("catalog version depends on file order")
	}
}

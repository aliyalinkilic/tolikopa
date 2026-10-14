package appcatalog

import (
	"os"
	"testing"
)

// TestCatalogLoads exercises Parse() against the embedded copy: bad kinds,
// malformed ids and duplicates fail here rather than panicking at server boot.
func TestCatalogLoads(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("catalog is empty")
	}
	if Version() == "" {
		t.Fatal("catalog version is empty")
	}
	var apps, startup int
	for _, it := range all {
		switch it.Kind {
		case KindApp:
			apps++
		case KindStartup:
			startup++
		}
	}
	if apps == 0 || startup == 0 {
		t.Fatalf("expected both app and startup items, got %d/%d", apps, startup)
	}
}

func TestLookupRejectsUnknownItems(t *testing.T) {
	if _, ok := Lookup(KindApp, "weather"); !ok {
		t.Error("expected to find the weather item")
	}
	// The admin panel can only select from the vocabulary; anything else must be
	// refused with 400 before it ever reaches remediation_policy_item.
	for _, id := range []string{"", "does-not-exist", "../etc/passwd", "WEATHER"} {
		if _, ok := Lookup(KindApp, id); ok {
			t.Errorf("Lookup(app, %q) unexpectedly succeeded", id)
		}
	}
	if _, ok := Lookup("bogus-kind", "weather"); ok {
		t.Error("Lookup with an unknown kind unexpectedly succeeded")
	}
}

// TestComputeVersionGoldenVector pins the hash ALGORITHM, not the current catalog
// contents (which change legitimately). agent/internal/appcatalog carries the
// IDENTICAL test with the IDENTICAL expected value. If the two implementations
// ever disagree, the drift handshake silently marks every deployed agent stale —
// this test is the guard against that.
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

// TestParseRejectsBadDocuments covers the paths synccatalog relies on to refuse a
// broken canonical file before it lands in the server binary.
func TestParseRejectsBadDocuments(t *testing.T) {
	cases := map[string]string{
		"not json":          `{`,
		"wrong schema":      `{"schema":2,"items":[]}`,
		"bad kind":          `{"schema":1,"items":[{"kind":"driver","item_id":"x","family":"f","display_name":"d"}]}`,
		"bad item_id":       `{"schema":1,"items":[{"kind":"app","item_id":"Weather!","family":"f","display_name":"d"}]}`,
		"missing family":    `{"schema":1,"items":[{"kind":"app","item_id":"weather","display_name":"d"}]}`,
		"duplicate":         `{"schema":1,"items":[{"kind":"app","item_id":"a","family":"f","display_name":"d"},{"kind":"app","item_id":"a","family":"g","display_name":"e"}]}`,
		"unsafe by default": `{"schema":1,"items":[{"kind":"app","item_id":"a","family":"f","display_name":"d","risk_tier":"advanced","safe_default":true}]}`,
	}
	for name, doc := range cases {
		if _, _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("Parse(%s) = nil error, want failure", name)
		}
	}
}

// TestEmbeddedCopyMatchesCanonical is the local half of the CI diff gate: it
// compares the embedded bytes with the agent's canonical file when the whole
// repository tree is present. It skips (rather than fails) when only the server
// module is mounted, which is how the standard build container runs.
func TestEmbeddedCopyMatchesCanonical(t *testing.T) {
	const canonical = "../../../agent/internal/appcatalog/catalog.json"
	want, err := os.ReadFile(canonical)
	if err != nil {
		t.Skipf("canonical catalog not mounted (%v); CI runs synccatalog + git diff instead", err)
	}
	if string(want) != string(catalogJSON) {
		t.Error("server/internal/appcatalog/catalog.json is stale — run `go run ./tools/synccatalog`")
	}
}

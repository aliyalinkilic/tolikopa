package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

func TestValidateKeepsOnlyCatalogItems(t *testing.T) {
	in := &model.RemediationPolicy{
		Revision: "r1",
		Enabled:  true,
		DryRun:   true,
		Apps:     []string{"weather", "does-not-exist", "news", "weather"},
		Startup:  []string{"startup.spotify", "startup.nope"},
	}
	got, rejected := Validate(in)

	if got.Revision != "r1" || !got.Enabled || !got.DryRun {
		t.Fatalf("scalar fields not preserved: %+v", got)
	}
	if len(got.Apps) != 2 || got.Apps[0] != "weather" || got.Apps[1] != "news" {
		t.Errorf("apps = %v, want [weather news] (duplicate collapsed)", got.Apps)
	}
	if len(got.Startup) != 1 || got.Startup[0] != "startup.spotify" {
		t.Errorf("startup = %v, want [startup.spotify]", got.Startup)
	}
	want := []string{"app:does-not-exist", "startup:startup.nope"}
	if len(rejected) != len(want) {
		t.Fatalf("rejected = %v, want %v", rejected, want)
	}
	for i := range want {
		if rejected[i] != want[i] {
			t.Errorf("rejected[%d] = %q, want %q", i, rejected[i], want[i])
		}
	}
}

func TestValidateRejectsEmptyRevision(t *testing.T) {
	for _, rev := range []string{"", "   "} {
		got, _ := Validate(&model.RemediationPolicy{Revision: rev, Enabled: true, Apps: []string{"weather"}})
		if got.Revision != "" || len(got.Apps) != 0 {
			t.Errorf("Validate(revision=%q) = %+v, want a wholesale refusal", rev, got)
		}
	}
	if got, _ := Validate(nil); got.Revision != "" {
		t.Error("Validate(nil) must refuse")
	}
}

func TestValidateRefusesOverCapLists(t *testing.T) {
	big := make([]string, MaxIDs+1)
	for i := range big {
		big[i] = "weather"
	}
	got, rejected := Validate(&model.RemediationPolicy{Revision: "r1", Enabled: true, Apps: big})
	if got.Revision != "" || len(got.Apps) != 0 || len(rejected) != 0 {
		t.Errorf("over-cap apps: got %+v / %v, want a wholesale refusal", got, rejected)
	}

	got, _ = Validate(&model.RemediationPolicy{Revision: "r1", Enabled: true, Startup: big})
	if got.Revision != "" {
		t.Error("over-cap startup list must refuse the whole policy")
	}

	// Exactly at the cap is fine (ids are deduplicated afterwards).
	atCap := make([]string, MaxIDs)
	for i := range atCap {
		atCap[i] = "weather"
	}
	if got, _ = Validate(&model.RemediationPolicy{Revision: "r1", Apps: atCap}); got.Revision != "r1" {
		t.Error("a list exactly at MaxIDs must be accepted")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "policy.json")
	s := NewStoreAt(path)

	if got := s.Load(); got.Policy.Revision != "" {
		t.Errorf("missing file should load the zero state, got %+v", got)
	}

	want := State{
		Policy: model.RemediationPolicy{
			Revision: "rev-7", Enabled: true, DryRun: true,
			Apps: []string{"weather"}, Startup: []string{"startup.spotify"},
		},
		Rejected:  []string{"app:unknown"},
		AppliedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := s.Current(); got.Policy.Revision != "rev-7" || len(got.Policy.Apps) != 1 {
		t.Errorf("in-memory state after Save = %+v", got)
	}

	fresh := NewStoreAt(path).Load()
	if fresh.Policy.Revision != want.Policy.Revision ||
		!fresh.Policy.Enabled || !fresh.Policy.DryRun ||
		len(fresh.Policy.Apps) != 1 || fresh.Policy.Apps[0] != "weather" ||
		len(fresh.Policy.Startup) != 1 || fresh.Policy.Startup[0] != "startup.spotify" ||
		len(fresh.Rejected) != 1 || fresh.Rejected[0] != "app:unknown" ||
		!fresh.AppliedAt.Equal(want.AppliedAt) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", fresh, want)
	}

	// The policy names machines' desired state; it must not be world readable.
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("policy.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestStoreLoadsCorruptFileAsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewStoreAt(path).Load(); got.Policy.Revision != "" || got.Policy.Enabled {
		t.Errorf("corrupt file must load as the zero state, got %+v", got)
	}
}

func TestSplitRejected(t *testing.T) {
	kind, id, ok := SplitRejected("app:weather")
	if !ok || kind != "app" || id != "weather" {
		t.Errorf("SplitRejected = %q/%q/%v", kind, id, ok)
	}
	for _, bad := range []string{"", "app", ":weather", "app:"} {
		if _, _, ok := SplitRejected(bad); ok {
			t.Errorf("SplitRejected(%q) unexpectedly ok", bad)
		}
	}
}

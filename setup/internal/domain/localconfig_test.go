package domain

import (
	"encoding/json"
	"testing"
)

func TestMergeConfigNeverTrustsBundleSwitches(t *testing.T) {
	bundle := []byte(`{"backend_url":"https://10.0.0.10","ingest_token":"t","allow_app_removal":true,"enforce_actions":true,"hygiene_hours":1}`)
	out, err := MergeConfig(bundle, nil, Switches{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"allow_app_removal", "allow_startup_control", "enforce_actions"} {
		if got[k] != false {
			t.Errorf("%s = %v, want false: a bundle must not be able to opt a fleet in", k, got[k])
		}
	}
	if _, ok := got["hygiene_hours"]; ok {
		t.Error("hygiene_hours taken from the bundle")
	}
	if got["backend_url"] != "https://10.0.0.10" || got["ingest_token"] != "t" {
		t.Errorf("connection settings lost: %v", got)
	}
}

// encoding/json matches struct fields with Unicode case folding, so U+017F (long s)
// reads as "s". Such a key must not survive the merge.
func TestMergeConfigDropsFoldedSwitchNames(t *testing.T) {
	bundle := []byte("{\"backend_url\":\"https://x\",\"allow_\u017ftartup_control\":true,\"enforce_action\u017f\":true,\"ALLOW_APP_REMOVAL\":true,\"extra\":1}")
	out, err := MergeConfig(bundle, nil, Switches{})
	if err != nil {
		t.Fatal(err)
	}
	var agentView struct {
		AllowAppRemoval     bool `json:"allow_app_removal"`
		AllowStartupControl bool `json:"allow_startup_control"`
		EnforceActions      bool `json:"enforce_actions"`
	}
	if err := json.Unmarshal(out, &agentView); err != nil {
		t.Fatal(err)
	}
	if agentView.AllowAppRemoval || agentView.AllowStartupControl || agentView.EnforceActions {
		t.Fatalf("a bundle key opted the fleet in: %+v\n%s", agentView, out)
	}
	var keys map[string]any
	_ = json.Unmarshal(out, &keys)
	if len(keys) != 4 { // backend_url + the three switches
		t.Errorf("unexpected keys survived: %v", keys)
	}
}

func TestMergeConfigKeepsLocalChoices(t *testing.T) {
	bundle := []byte(`{"backend_url":"https://new","ingest_token":"t2"}`)
	existing := []byte(`{"backend_url":"https://old","allow_app_removal":true,"hygiene_hours":12}`)
	out, err := MergeConfig(bundle, existing, Switches{AllowAppRemoval: true})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["backend_url"] != "https://new" || got["allow_app_removal"] != true || got["hygiene_hours"] != float64(12) {
		t.Errorf("unexpected merge: %v", got)
	}
	again, _ := MergeConfig(bundle, out, Switches{AllowAppRemoval: true})
	if string(again) != string(out) {
		t.Error("merge is not stable across runs; every run would restart every agent")
	}
	if _, err := MergeConfig([]byte("not json"), nil, Switches{}); err == nil {
		t.Error("invalid bundle config accepted")
	}
}

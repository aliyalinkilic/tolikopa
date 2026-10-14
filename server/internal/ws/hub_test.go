package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// The panel (web/src/lib/types.ts WSEnvelope, hooks/useMachineLive.ts) reads
// exactly these keys from a machine.metric push.
func TestMetricMessageShape(t *testing.T) {
	ts := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	msg := metricMessage(model.FleetMachine{MachineID: "m1", LastSeen: ts, CPUPct: 12.5, MemRatio: 0.5})
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "machine.metric" {
		t.Fatalf("type = %q", got.Type)
	}
	want := map[string]any{"machine_id": "m1", "t": "2026-10-14T09:00:00Z", "cpu_pct": 12.5, "mem_ratio": 0.5}
	if len(got.Payload) != len(want) {
		t.Fatalf("payload keys = %v, want %v", got.Payload, want)
	}
	for k, v := range want {
		if got.Payload[k] != v {
			t.Fatalf("payload[%q] = %v, want %v", k, got.Payload[k], v)
		}
	}
}

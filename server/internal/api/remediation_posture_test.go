package api

import (
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func fp(v float64) *float64 { return &v }

func adviceKeys(a []model.LinuxAdvice) map[string]string {
	out := map[string]string{}
	for _, x := range a {
		out[x.Key] = x.Severity
	}
	return out
}

func TestLinuxAdvice(t *testing.T) {
	// Nothing measured → no advice (never a confident "fine" either; the panel
	// shows the missing numbers as unknown).
	if got := linuxAdvice(model.LinuxAdvisoryMachine{}); len(got) != 0 {
		t.Fatalf("unmeasured host got advice: %+v", got)
	}

	full := model.LinuxAdvisoryMachine{
		SMARTBad: true, DiskUsedPct: fp(97), MemRatioAvg: fp(0.9), CPUAvg: fp(80),
		SampleHours: advisoryMinHours, UptimeDays: 120,
	}
	got := adviceKeys(linuxAdvice(full))
	want := map[string]string{
		"smart": "crit", "disk_critical": "crit", "mem_pressure": "warn",
		"cpu_sustained": "warn", "long_uptime": "warn",
	}
	for k, sev := range want {
		if got[k] != sev {
			t.Errorf("%s: got %q, want %q (all: %v)", k, got[k], sev, got)
		}
	}
	if _, dup := got["disk_low"]; dup {
		t.Error("disk_low emitted alongside disk_critical")
	}

	// A thin sample withholds CPU/RAM advice; disk and uptime still apply.
	thin := full
	thin.SampleHours = advisoryMinHours - 1
	thin.DiskUsedPct = fp(88)
	got = adviceKeys(linuxAdvice(thin))
	if _, ok := got["mem_pressure"]; ok {
		t.Error("mem_pressure on a thin sample")
	}
	if _, ok := got["cpu_sustained"]; ok {
		t.Error("cpu_sustained on a thin sample")
	}
	if got["disk_low"] != "warn" {
		t.Errorf("disk_low missing: %v", got)
	}
}

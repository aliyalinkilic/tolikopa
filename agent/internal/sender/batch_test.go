package sender

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

func TestAddAppFocusBoundedAndValidated(t *testing.T) {
	s := &Sender{}
	for i := 0; i < 3*maxTrackedApps; i++ {
		s.AddAppFocus(fmt.Sprintf("junk%d", i), 1)
	}
	s.AddAppFocus("real", 1) // arrives after the accumulator is full: ignored
	if got := len(s.appFocus); got != maxTrackedApps {
		t.Fatalf("tracked apps = %d, want %d", got, maxTrackedApps)
	}

	s = &Sender{}
	for _, bad := range []string{"", "__idle", `c:\users\x\a.exe`, "a/b", strings.Repeat("a", 129), "a\x00b", "a\u202eb", "\xff"} {
		s.AddAppFocus(bad, 10)
	}
	if len(s.appFocus) != 0 {
		t.Fatalf("invalid names accepted: %v", s.appFocus)
	}
	s.AddAppFocus("Chrome.EXE", 10)
	s.AddAppFocus("chrome", maxFocusSeconds)
	if got := s.appFocus["chrome"]; got != maxFocusSeconds {
		t.Fatalf("chrome = %d, want normalised and clamped to %d", got, maxFocusSeconds)
	}
}

func TestSwapKeepsTopFocus(t *testing.T) {
	s := &Sender{}
	for i := 0; i < 500; i++ {
		s.AddAppFocus(fmt.Sprintf("app%03d", i), i+1)
	}
	p := s.swap()
	if len(p.focus) != srvMaxAppFocusRows {
		t.Fatalf("focus rows = %d, want %d", len(p.focus), srvMaxAppFocusRows)
	}
	if p.focus[0].Seconds != 500 || p.focus[len(p.focus)-1].Seconds != 301 {
		t.Fatalf("did not keep the most-seconds entries: first=%d last=%d", p.focus[0].Seconds, p.focus[len(p.focus)-1].Seconds)
	}
}

func TestChunkRespectsServerCaps(t *testing.T) {
	procs := make([]model.Process, 100)
	disks := make([]model.Disk, 50)
	var p parts
	for i := 0; i < 1000; i++ {
		p.samples = append(p.samples, model.Sample{Processes: procs, Disks: disks})
	}
	p.events = make([]model.Event, 1700)
	p.focus = make([]model.AppFocus, 300)
	p.rem = &model.RemediationReport{
		Present:  make([]model.RemediationItem, 1000),
		Attempts: make([]model.RemediationAttempt, 1000),
	}

	chunks := chunk(p)
	var samples, events int
	for i, c := range chunks {
		if len(c.samples) > srvMaxSamples || len(c.samples) > maxBatchSamples {
			t.Errorf("chunk %d: %d samples", i, len(c.samples))
		}
		rows := 0
		for _, s := range c.samples {
			if len(s.Disks) > srvMaxDisksPerSample {
				t.Errorf("chunk %d: %d disks in a sample", i, len(s.Disks))
			}
			rows += len(s.Processes)
		}
		if rows > srvMaxProcessRows {
			t.Errorf("chunk %d: %d process rows", i, rows)
		}
		if len(c.events) > maxEnvelopeEvents {
			t.Errorf("chunk %d: %d events", i, len(c.events))
		}
		if len(c.focus) > srvMaxAppFocusRows {
			t.Errorf("chunk %d: %d focus rows", i, len(c.focus))
		}
		if c.rem != nil && (len(c.rem.Present) > srvMaxRemediationItems || len(c.rem.Attempts) > srvMaxRemediationAttempts) {
			t.Errorf("chunk %d: remediation over cap", i)
		}
		if i > 0 && (c.rem != nil || len(c.focus) > 0) {
			t.Errorf("chunk %d: single-valued data duplicated", i)
		}
		samples += len(c.samples)
		events += len(c.events)
	}
	if samples != 1000 || events != 1700 {
		t.Fatalf("lost data: samples=%d events=%d", samples, events)
	}
	// The caller's report must not be mutated by the cap.
	if len(p.rem.Present) != 1000 {
		t.Fatalf("capRemediation mutated its input")
	}

	if got := chunk(parts{}); len(got) != 1 || !got[0].empty() {
		t.Fatalf("empty buffer must yield one heartbeat chunk, got %d", len(got))
	}
}

func TestSplitTerminates(t *testing.T) {
	p := parts{
		samples: make([]model.Sample, 7),
		events:  make([]model.Event, 5),
		focus:   make([]model.AppFocus, 3),
		upd:     &model.UpdateStatus{},
	}
	var walk func(p parts, depth int) (int, int, int)
	walk = func(p parts, depth int) (int, int, int) {
		if depth > 20 {
			t.Fatal("split does not terminate")
		}
		h := p.split()
		if h == nil {
			return len(p.samples), len(p.events), len(p.focus)
		}
		var s, e, f int
		for _, x := range h {
			a, b, c := walk(x, depth+1)
			s, e, f = s+a, e+b, f+c
		}
		return s, e, f
	}
	if s, e, f := walk(p, 0); s != 7 || e != 5 || f != 3 {
		t.Fatalf("split lost or duplicated data: %d %d %d", s, e, f)
	}
}

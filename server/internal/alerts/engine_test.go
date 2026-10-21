package alerts

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/workhours"
)

func fp(v float64) *float64 { return &v }

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, workhours.Location)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Seeded offline thresholds: warn 300 s, crit 1800 s. 2026-10-20 is a Tuesday.
func TestDecideOfflineServer24x7(t *testing.T) {
	s := workhours.Compile(workhours.Default())
	tags := []string{"Sanal Makine"}
	// A server off at 03:00 on a Saturday still alerts, at full severity.
	d := decideOffline(fp(300), fp(1800), s, tags, at(t, "2026-10-24 03:40"), at(t, "2026-10-24 03:00"), false, "")
	if d.sev != "critical" || d.workHours || d.clear {
		t.Fatalf("server: %+v", d)
	}
	d = decideOffline(fp(300), fp(1800), s, nil, at(t, "2026-10-24 03:06"), at(t, "2026-10-24 03:00"), false, "")
	if d.sev != "warning" || d.thr != 300 {
		t.Fatalf("untagged: %+v", d)
	}
}

func TestDecideOfflineDefaultHasNoWorkHoursDevices(t *testing.T) {
	// The default tag list is empty: every machine is monitored 24/7.
	s := workhours.Compile(workhours.Default())
	d := decideOffline(fp(300), fp(1800), s, []string{"Personel"}, at(t, "2026-10-24 03:40"), at(t, "2026-10-24 03:00"), false, "")
	if d.sev != "critical" || d.workHours {
		t.Fatalf("untagged default: %+v", d)
	}
}

func TestDecideOfflineWorkHours(t *testing.T) {
	w := workhours.Default() // Mon–Fri 09:00–17:30, grace 60, offline_after 60, stale 1
	w.Tags = []string{"Office"}
	s := workhours.Compile(w)
	tags := []string{"office"} // case-insensitive match of "Office"
	yesterday := at(t, "2026-10-19 17:25")
	cases := []struct {
		name       string
		now        string
		seen       time.Time
		open       bool
		sev        string
		kind       string
		clear      bool
		wantThresh float64
	}{
		{"off at 17:35 – no alarm", "2026-10-19 17:35", at(t, "2026-10-19 17:28"), false, "", "", true, 0},
		{"overnight", "2026-10-20 02:00", yesterday, false, "", "", true, 0},
		{"open alert at end of day is cleared", "2026-10-19 17:30", at(t, "2026-10-19 15:00"), true, "", "", true, 0},
		{"morning boot-up grace", "2026-10-20 09:40", yesterday, false, "", "", false, 0},
		{"not switched on by 10:00 – info", "2026-10-20 10:00", yesterday, false, "info", "not_started", false, 3600},
		{"not switched on all day – still info", "2026-10-20 15:00", yesterday, false, "info", "not_started", false, 3600},
		{"lunch break under 60 min", "2026-10-20 12:50", at(t, "2026-10-20 12:00"), false, "", "", false, 0},
		{"off 70 min midday – warning", "2026-10-20 13:10", at(t, "2026-10-20 12:00"), false, "warning", "went_offline", false, 3600},
		{"left early: evening grace opens nothing", "2026-10-20 17:10", at(t, "2026-10-20 16:00"), false, "", "", false, 0},
		{"but keeps an alert already open", "2026-10-20 17:10", at(t, "2026-10-20 15:00"), true, "warning", "went_offline", false, 3600},
		{"weekend", "2026-10-24 11:00", at(t, "2026-10-23 17:00"), true, "", "", true, 0},
	}
	for _, c := range cases {
		d := decideOffline(fp(300), fp(1800), s, tags, at(t, c.now), c.seen, c.open, "")
		if d.sev != c.sev || d.kind != c.kind || d.clear != c.clear || !d.workHours || (c.sev != "" && d.thr != c.wantThresh) {
			t.Errorf("%s: got %+v, want sev=%q kind=%q clear=%v thr=%v", c.name, d, c.sev, c.kind, c.clear, c.wantThresh)
		}
		if d.clear && d.reason != "work_hours_end" {
			t.Errorf("%s: reason %q", c.name, d.reason)
		}
	}
	// A rule warn threshold longer than offline_after wins.
	d := decideOffline(fp(7200), nil, s, tags, at(t, "2026-10-20 13:10"), at(t, "2026-10-20 12:00"), false, "")
	if d.sev != "" {
		t.Fatalf("70 min < 120 min warn: %+v", d)
	}
	// Rule without thresholds never fires (before the device turns stale).
	if d := decideOffline(nil, nil, s, tags, at(t, "2026-10-20 15:00"), yesterday, false, ""); d.sev != "" {
		t.Fatalf("no thresholds: %+v", d)
	}
}

func TestDecideOfflineStale(t *testing.T) {
	w := workhours.Default() // stale after 1 missed work day
	w.Tags = []string{"Office"}
	s := workhours.Compile(w)
	tags := []string{"Office"}
	mondayEvening := at(t, "2026-10-19 17:25")
	off := func(now string, seen time.Time, open bool, kind string) offlineVerdict {
		return decideOffline(fp(300), fp(1800), s, tags, at(t, now), seen, open, kind)
	}
	// Tuesday missed entirely: the not_started alert turns stale when the day
	// ends instead of being cleared …
	if d := off("2026-10-20 17:30", mondayEvening, true, "not_started"); d.clear || d.sev != "info" || d.kind != "stale" {
		t.Fatalf("end of the first missed day: %+v", d)
	}
	// … and stays open around the clock: overnight, at the weekend and inside
	// the morning grace, without a new alert every workday.
	for _, now := range []string{"2026-10-21 02:00", "2026-10-21 09:30", "2026-10-21 11:00", "2026-10-24 12:00", "2026-11-20 10:00"} {
		if d := off(now, mondayEvening, true, "stale"); d.clear || d.sev != "info" || d.kind != "stale" {
			t.Errorf("%s: stale must hold: %+v", now, d)
		}
	}
	// A stale device opens its alert once even when none is open (e.g. the
	// server was down at the transition), regardless of the time of day.
	if d := off("2026-10-22 03:00", mondayEvening, false, ""); d.sev != "info" || d.kind != "stale" {
		t.Fatalf("open once: %+v", d)
	}
	// It reported again: outside hours the stale alert resolves at once with
	// its own reason; inside hours it is simply no longer active (the engine
	// resolves it after the usual grace).
	seenAgain := at(t, "2026-10-22 18:40")
	if d := off("2026-10-22 18:45", seenAgain, true, "stale"); !d.clear || d.reason != "not_stale" {
		t.Fatalf("back outside hours: %+v", d)
	}
	if d := off("2026-10-23 10:00", at(t, "2026-10-23 09:55"), true, "stale"); d.clear || d.sev != "" {
		t.Fatalf("back inside hours: %+v", d)
	}
	// Servers never turn stale.
	if d := decideOffline(fp(300), fp(1800), s, []string{"Sunucu"}, at(t, "2026-11-20 10:00"), mondayEvening, true, ""); d.sev != "critical" || d.kind != "" {
		t.Fatalf("server: %+v", d)
	}
	// A longer cutoff: 3 missed days.
	w.StaleAfterDays = 3
	s3 := workhours.Compile(w)
	if d := decideOffline(fp(300), fp(1800), s3, tags, at(t, "2026-10-22 11:00"), mondayEvening, true, "not_started"); d.kind != "not_started" {
		t.Fatalf("2 missed days < 3: %+v", d)
	}
	if d := decideOffline(fp(300), fp(1800), s3, tags, at(t, "2026-10-22 17:30"), mondayEvening, true, "not_started"); d.kind != "stale" {
		t.Fatalf("3 missed days: %+v", d)
	}
}

func TestOfflineKindMessages(t *testing.T) {
	for kind, want := range map[string][2]string{
		"not_started":  {"bugün hiç açılmadı", "not been switched on today"},
		"went_offline": {"gün içinde çevrimdışı oldu", "went offline during the day"},
		"stale":        {"kullanılmıyorsa arşivleyin", "archive it"},
	} {
		p := Params{"hostname": "PC-01", "kind": kind, "minutes": 70, "days": 12}
		if tr := RenderMessage(LangTR, "offline", p); !strings.Contains(tr, want[0]) {
			t.Errorf("%s/tr: %q", kind, tr)
		}
		if en := RenderMessage(LangEN, "offline", p); !strings.Contains(en, want[1]) {
			t.Errorf("%s/en: %q", kind, en)
		}
	}
}

func TestHysteresis(t *testing.T) {
	warn, crit := fp(95), fp(98)
	cases := []struct {
		v    float64
		open bool
		want string
	}{
		{94, false, ""},
		{95, false, "warning"},
		{98.5, false, "critical"},
		{91, true, "warning"}, // open: holds until below 90
		{89.9, true, ""},
		{97, true, "warning"}, // de-escalates from critical, stays open
	}
	for _, c := range cases {
		if got, _ := sevHighHyst(c.v, warn, crit, c.open, usageHysteresis); got != c.want {
			t.Errorf("sevHighHyst(%v, open=%v) = %q, want %q", c.v, c.open, got, c.want)
		}
	}
	// Disk: warn at 15 % free, crit at 5 %, margin 2.
	dw, dc := fp(15), fp(5)
	for _, c := range []struct {
		v    float64
		open bool
		want string
	}{{15.5, false, ""}, {14, false, "warning"}, {4, false, "critical"}, {16.9, true, "warning"}, {17.1, true, ""}} {
		if got, _ := sevLowHyst(c.v, dw, dc, c.open, diskHysteresis); got != c.want {
			t.Errorf("sevLowHyst(%v, open=%v) = %q, want %q", c.v, c.open, got, c.want)
		}
	}
	// Crit-only rule: open alert holds at critical within the margin.
	if got, _ := sevHighHyst(96, nil, fp(98), true, 5); got != "critical" {
		t.Errorf("crit-only hysteresis: %q", got)
	}
}

func TestHoldSilent(t *testing.T) {
	open := map[string]string{"disk_low:a:C:": "a", "disk_low:b:C:": "b", "disk_low:b:D:": "b", "x": ""}
	got := holdSilent(open, map[string]bool{"a": true})
	sort.Strings(got)
	if len(got) != 2 || got[0] != "disk_low:b:C:" || got[1] != "disk_low:b:D:" {
		t.Fatalf("holdSilent = %v", got)
	}
}

func TestIgnoredApps(t *testing.T) {
	got := ignoredApps(map[string]any{"ignore_apps": []any{" VendorSvc64.exe ", "oemhelperservice", 3, ""}})
	if len(got) != 2 || got[0] != "vendorsvc64" || got[1] != "oemhelperservice" {
		t.Fatalf("ignoredApps = %v", got)
	}
	if got := ignoredApps(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil params: %v (must be a non-nil empty slice for ANY($2))", got)
	}
}

func TestResolveGraceFor(t *testing.T) {
	if resolveGraceFor("crash_storm") != time.Hour || resolveGraceFor("offline") != resolveGrace {
		t.Fatal("resolve grace per rule")
	}
}

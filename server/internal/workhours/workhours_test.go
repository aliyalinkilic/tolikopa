package workhours

import (
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// ist builds an Istanbul wall-clock time. 2026-10-19 is a Monday.
func ist(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, Location)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLocationIsIstanbul(t *testing.T) {
	if Location.String() != TimezoneName {
		t.Fatalf("Location = %q, want %q (tzdata not embedded?)", Location, TimezoneName)
	}
	// Istanbul is UTC+3 all year.
	for _, s := range []string{"2026-01-15 12:00", "2026-07-15 12:00"} {
		if _, off := ist(t, s).Zone(); off != 3*3600 {
			t.Fatalf("%s offset = %d, want +3h", s, off)
		}
	}
}

func TestNormalize(t *testing.T) {
	n, err := Normalize(model.WorkHours{
		Days: []int{5, 1, 3, 1}, Start: "08:30", End: "18:00",
		Tags: []string{" Personel ", "PERSONEL", "", "Lab"}, GraceMinutes: 60, OfflineAfterMinutes: 30, StaleAfterDays: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Days; len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("days = %v", got)
	}
	if len(n.Tags) != 2 || n.Tags[0] != "Personel" || n.Tags[1] != "Lab" {
		t.Fatalf("tags = %v", n.Tags)
	}
	if n.Timezone != TimezoneName {
		t.Fatalf("timezone = %q", n.Timezone)
	}

	bad := []model.WorkHours{
		{Days: []int{0}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 30},
		{Days: []int{8}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "8:30", End: "18:00", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "08:60", End: "18:00", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "18:00", End: "08:30", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "09:00", End: "09:00", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "24:00", End: "24:00", OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "08:30", End: "18:00", GraceMinutes: -1, OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "08:30", End: "18:00", GraceMinutes: 241, OfflineAfterMinutes: 30},
		{Days: []int{1}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 4},
		{Days: []int{1}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 30, StaleAfterDays: 1, Tags: []string{"a\nb"}},
		{Days: []int{1}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 30, StaleAfterDays: 0},
		{Days: []int{1}, Start: "08:30", End: "18:00", OfflineAfterMinutes: 30, StaleAfterDays: 31},
	}
	for i, w := range bad {
		if _, err := Normalize(w); err == nil {
			t.Errorf("case %d: %+v accepted", i, w)
		}
	}
	// 24:00 is a valid end of day.
	if _, err := Normalize(model.WorkHours{Days: []int{1}, Start: "00:00", End: "24:00", OfflineAfterMinutes: 30, StaleAfterDays: 30}); err != nil {
		t.Fatalf("00:00–24:00 rejected: %v", err)
	}
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.Start != "09:00" || d.End != "17:30" || d.GraceMinutes != 60 || d.OfflineAfterMinutes != 60 || d.StaleAfterDays != 1 {
		t.Fatalf("Default() = %+v, want 09:00–17:30, grace 60, offline_after 60, stale 1", d)
	}
	if d.Tags == nil || len(d.Tags) != 0 {
		t.Fatalf("Default().Tags = %#v, want a non-nil empty list", d.Tags)
	}
	if _, err := Normalize(d); err != nil {
		t.Fatalf("Default() does not normalize: %v", err)
	}
	// No tags: no machine is work-hours-only (24/7 monitoring until configured).
	s := Compile(d)
	for _, tags := range [][]string{nil, {}, {"Personel"}, {"Office"}} {
		if s.Applies(tags) {
			t.Errorf("default schedule applies to %v", tags)
		}
	}
}

func TestDecodeFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{``, `"x"`, `{"days":[9],"start":"08:30","end":"18:00"}`} {
		got := Decode([]byte(raw))
		if got.Start != defaultStart || got.End != defaultEnd || len(got.Tags) != 0 {
			t.Fatalf("Decode(%q) = %+v, want default", raw, got)
		}
	}
}

// office is the default schedule with one work-hours tag configured.
func office() Schedule {
	w := Default()
	w.Tags = []string{"Office"}
	return Compile(w)
}

func TestAppliesCaseInsensitive(t *testing.T) {
	s := office()
	for _, tags := range [][]string{{"Office"}, {"office"}, {"Sanal Makine", "OFFICE "}} {
		if !s.Applies(tags) {
			t.Errorf("Applies(%v) = false", tags)
		}
	}
	for _, tags := range [][]string{nil, {}, {"Sunucu"}, {"Offices"}} {
		if s.Applies(tags) {
			t.Errorf("Applies(%v) = true", tags)
		}
	}
}

func TestWindowAndDayBoundaries(t *testing.T) {
	s := Compile(Default()) // Mon–Fri 09:00–17:30
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-10-19 08:59", false}, // Monday before start
		{"2026-10-19 09:00", true},  // start is inclusive
		{"2026-10-19 17:29", true},
		{"2026-10-19 17:30", false}, // end is exclusive
		{"2026-10-23 12:00", true},  // Friday
		{"2026-10-24 12:00", false}, // Saturday
		{"2026-10-25 12:00", false}, // Sunday (ISO 7)
		{"2026-10-20 00:00", false}, // midnight Tuesday
	}
	for _, c := range cases {
		if got := s.InHours(ist(t, c.at)); got != c.want {
			t.Errorf("InHours(%s) = %v, want %v", c.at, got, c.want)
		}
	}
	// The day is the ISTANBUL day: Sunday 22:30 UTC is already Monday 01:30 local.
	sunUTC := time.Date(2026, 10, 18, 22, 30, 0, 0, time.UTC)
	start, _, ok := s.Window(sunUTC)
	if !ok || !start.Equal(ist(t, "2026-10-19 09:00")) {
		t.Fatalf("Window(Sun 22:30Z) = %v %v, want Monday 09:00 local", start, ok)
	}
	// Friday 21:30 UTC is Saturday 00:30 local: no window.
	if _, _, ok := s.Window(time.Date(2026, 10, 23, 21, 30, 0, 0, time.UTC)); ok {
		t.Fatal("Saturday local reported as a workday")
	}
}

func TestOfflinePresence(t *testing.T) {
	s := Compile(Default()) // 09:00–17:30, grace 60: new alerts open 10:00–16:30
	lastEvening := ist(t, "2026-10-19 17:25")
	cases := []struct {
		name     string
		now      string
		seen     time.Time
		in, open bool
		age      time.Duration
	}{
		// Turned off at 17:25, evaluated at 17:35: outside hours, no alert.
		{"after end", "2026-10-19 17:35", lastEvening, false, false, 0},
		{"night", "2026-10-20 03:00", lastEvening, false, false, 0},
		{"before start", "2026-10-20 08:30", lastEvening, false, false, 0},
		// Inside the morning grace: in hours (age counts from 09:00) but no new alert.
		{"morning grace", "2026-10-20 09:30", lastEvening, true, false, 30 * time.Minute},
		// Still off at 10:00 on a workday: may alert; offline since 09:00 = 60 min.
		{"after grace", "2026-10-20 10:00", lastEvening, true, true, 60 * time.Minute},
		// Seen at 11:00, evaluated 11:20: age is from last seen.
		{"midday", "2026-10-20 11:20", ist(t, "2026-10-20 11:00"), true, true, 20 * time.Minute},
		// Evening grace: 16:30–17:30 opens nothing new.
		{"evening grace", "2026-10-20 16:40", ist(t, "2026-10-20 16:00"), true, false, 40 * time.Minute},
		{"last open minute", "2026-10-20 16:29", ist(t, "2026-10-20 15:00"), true, true, 89 * time.Minute},
		{"saturday", "2026-10-24 10:00", lastEvening, false, false, 0},
		// Future last_seen (clock skew) clamps to zero.
		{"skew", "2026-10-20 10:00", ist(t, "2026-10-20 10:01"), true, true, 0},
	}
	for _, c := range cases {
		p := s.OfflinePresence(ist(t, c.now), c.seen)
		if p.InHours != c.in || p.MayOpen != c.open || p.Age != c.age {
			t.Errorf("%s: got %+v, want in=%v open=%v age=%v", c.name, p, c.in, c.open, c.age)
		}
	}
}

func TestZeroGrace(t *testing.T) {
	w := Default()
	w.GraceMinutes = 0
	s := Compile(w)
	p := s.OfflinePresence(ist(t, "2026-10-19 09:00"), ist(t, "2026-10-16 17:00"))
	if !p.InHours || !p.MayOpen || p.Age != 0 {
		t.Fatalf("got %+v", p)
	}
	p = s.OfflinePresence(ist(t, "2026-10-19 17:29"), ist(t, "2026-10-19 17:00"))
	if !p.MayOpen {
		t.Fatalf("17:29 with zero grace: %+v", p)
	}
}

func TestDecodeBackfillsStaleAfterDays(t *testing.T) {
	// A schedule saved before stale_after_days existed keeps its other fields.
	got := Decode([]byte(`{"days":[1,2],"start":"08:00","end":"16:00","tags":["Lab"],"grace_minutes":30,"offline_after_minutes":45}`))
	if got.Start != "08:00" || len(got.Tags) != 1 || got.StaleAfterDays != defaultStaleDays {
		t.Fatalf("Decode(old row) = %+v", got)
	}
	got = Decode([]byte(`{"days":[1],"start":"08:00","end":"16:00","tags":[],"grace_minutes":30,"offline_after_minutes":45,"stale_after_days":7}`))
	if got.StaleAfterDays != 7 {
		t.Fatalf("stale_after_days = %d, want 7", got.StaleAfterDays)
	}
}

func TestMissedWorkdaysAndStale(t *testing.T) {
	s := office() // Mon–Fri 09:00–17:30, stale after 1 missed day
	friEvening := ist(t, "2026-10-23 17:00")
	cases := []struct {
		name string
		now  string
		seen time.Time
		want int
	}{
		{"weekend does not count", "2026-10-25 23:00", friEvening, 0},
		{"Monday window still running", "2026-10-26 12:00", friEvening, 0},
		{"Monday missed once it ends", "2026-10-26 17:30", friEvening, 1},
		{"seen Monday morning: Monday not missed", "2026-10-26 18:00", ist(t, "2026-10-26 09:10"), 0},
		{"seen exactly at start: not missed", "2026-10-26 18:00", ist(t, "2026-10-26 09:00"), 0},
		{"off since Monday noon, Tuesday missed", "2026-10-27 17:30", ist(t, "2026-10-26 12:00"), 1},
		{"now before last seen", "2026-10-20 10:00", ist(t, "2026-10-21 10:00"), 0},
	}
	for _, c := range cases {
		if got := s.MissedWorkdays(ist(t, c.now), c.seen, 10); got != c.want {
			t.Errorf("%s: MissedWorkdays = %d, want %d", c.name, got, c.want)
		}
	}
	// Two full weeks of silence is ten workdays; the count stops at limit.
	if got := s.MissedWorkdays(ist(t, "2026-11-06 18:00"), friEvening, 30); got != 10 {
		t.Errorf("two weeks: %d, want 10", got)
	}
	if got := s.MissedWorkdays(ist(t, "2027-10-23 18:00"), friEvening, 3); got != 3 {
		t.Errorf("a year capped at limit 3: %d", got)
	}
	if s.Stale(ist(t, "2026-10-26 17:29"), friEvening) || !s.Stale(ist(t, "2026-10-26 17:30"), friEvening) {
		t.Error("stale must flip when the first missed window ends")
	}
	// No workdays at all: never stale (and no endless loop).
	w := Default()
	w.Days = []int{}
	w.Tags = []string{"Office"}
	if Compile(w).Stale(ist(t, "2027-10-23 18:00"), friEvening) {
		t.Error("no workdays: stale")
	}
}

func TestOfflinePresenceNotStarted(t *testing.T) {
	s := office()
	if p := s.OfflinePresence(ist(t, "2026-10-20 10:00"), ist(t, "2026-10-19 17:25")); !p.NotStarted {
		t.Errorf("off since yesterday: %+v", p)
	}
	if p := s.OfflinePresence(ist(t, "2026-10-20 13:00"), ist(t, "2026-10-20 09:05")); p.NotStarted {
		t.Errorf("seen this morning: %+v", p)
	}
}

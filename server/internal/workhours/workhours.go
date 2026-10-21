// Package workhours holds the office schedule (app_setting "work_hours") and the
// pure decision logic the alert engine uses for work-hours-only devices: a staff
// PC switched off at 17:35 is not an incident, one still off at 10:00 on a
// workday is.
//
// All wall-clock arithmetic happens in Europe/Istanbul. The tz database is
// embedded (time/tzdata) so the decision never depends on the container image
// shipping /usr/share/zoneinfo.
package workhours

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Istanbul must resolve in a scratch/distroless image

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// SettingKey is the app_setting row holding the schedule as JSON.
const SettingKey = "work_hours"

// TimezoneName is the only zone schedules are interpreted in.
const TimezoneName = "Europe/Istanbul"

// Validation limits.
const (
	MaxTags            = 32
	MaxTagLen          = 64
	MaxGraceMinutes    = 240
	MinOfflineMinutes  = 5
	MaxOfflineMinutes  = 480
	MinStaleDays       = 1
	MaxStaleDays       = 30
	defaultStart       = "09:00"
	defaultEnd         = "17:30"
	defaultGrace       = 60
	defaultOfflineMins = 60
	defaultStaleDays   = 1
)

// Location is Europe/Istanbul. With the embedded tzdata LoadLocation cannot
// fail; the fixed +03:00 fallback (Türkiye has no DST since 2016) is belt and braces.
var Location = func() *time.Location {
	if loc, err := time.LoadLocation(TimezoneName); err == nil {
		return loc
	}
	return time.FixedZone("+03", 3*60*60)
}()

// Default is the schedule used until an admin saves one: Mon–Fri 09:00–17:30,
// grace 60 min (new offline alerts may open 10:00–16:30), offline_after 60 min,
// stale after 1 missed work day.
// The tag list is empty: no machine is work-hours-only, so every machine is
// monitored 24/7 until an admin picks the tags in the general settings.
func Default() model.WorkHours {
	return model.WorkHours{
		Days:                []int{1, 2, 3, 4, 5},
		Start:               defaultStart,
		End:                 defaultEnd,
		Tags:                []string{},
		GraceMinutes:        defaultGrace,
		OfflineAfterMinutes: defaultOfflineMins,
		StaleAfterDays:      defaultStaleDays,
		Timezone:            TimezoneName,
	}
}

// ErrInvalid wraps every validation failure; Field names the offending field.
type ErrInvalid struct {
	Field  string
	Reason string
}

func (e *ErrInvalid) Error() string { return "work_hours." + e.Field + ": " + e.Reason }

func invalid(field, reason string) error { return &ErrInvalid{Field: field, Reason: reason} }

// parseHHMM parses "HH:MM" (00:00–23:59, also accepts "24:00" for end-of-day)
// into minutes after midnight.
func parseHHMM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	for i, c := range s {
		if i == 2 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if m > 59 || h > 24 || (h == 24 && m != 0) {
		return 0, false
	}
	return h*60 + m, true
}

// Normalize validates w and returns its canonical form: days sorted and
// de-duplicated, times "HH:MM", tags trimmed and de-duplicated
// case-insensitively (first spelling wins), Timezone set.
func Normalize(w model.WorkHours) (model.WorkHours, error) {
	out := model.WorkHours{Timezone: TimezoneName, Days: []int{}, Tags: []string{}}
	var seenDay [8]bool
	for _, d := range w.Days {
		if d < 1 || d > 7 {
			return out, invalid("days", "weekday must be 1 (Monday) … 7 (Sunday)")
		}
		seenDay[d] = true
	}
	for d := 1; d <= 7; d++ {
		if seenDay[d] {
			out.Days = append(out.Days, d)
		}
	}
	start, ok := parseHHMM(w.Start)
	if !ok || start >= 24*60 {
		return out, invalid("start", "must be HH:MM")
	}
	end, ok := parseHHMM(w.End)
	if !ok {
		return out, invalid("end", "must be HH:MM")
	}
	if end <= start {
		return out, invalid("end", "must be after start (overnight schedules are not supported)")
	}
	out.Start = fmt.Sprintf("%02d:%02d", start/60, start%60)
	out.End = fmt.Sprintf("%02d:%02d", end/60, end%60)

	if w.GraceMinutes < 0 || w.GraceMinutes > MaxGraceMinutes {
		return out, invalid("grace_minutes", fmt.Sprintf("must be 0 … %d", MaxGraceMinutes))
	}
	out.GraceMinutes = w.GraceMinutes
	if w.OfflineAfterMinutes < MinOfflineMinutes || w.OfflineAfterMinutes > MaxOfflineMinutes {
		return out, invalid("offline_after_minutes", fmt.Sprintf("must be %d … %d", MinOfflineMinutes, MaxOfflineMinutes))
	}
	out.OfflineAfterMinutes = w.OfflineAfterMinutes
	if w.StaleAfterDays < MinStaleDays || w.StaleAfterDays > MaxStaleDays {
		return out, invalid("stale_after_days", fmt.Sprintf("must be %d … %d", MinStaleDays, MaxStaleDays))
	}
	out.StaleAfterDays = w.StaleAfterDays

	seen := map[string]bool{}
	for _, t := range w.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len(t) > MaxTagLen || strings.ContainsFunc(t, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return out, invalid("tags", "tag is too long or contains control characters")
		}
		k := foldTag(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out.Tags = append(out.Tags, t)
	}
	if len(out.Tags) > MaxTags {
		return out, invalid("tags", fmt.Sprintf("at most %d tags", MaxTags))
	}
	return out, nil
}

// foldTag is the case-insensitive comparison key ("PERSONEL" == "personel").
// strings.ToLower is Unicode-aware; Turkish dotted/dotless i are compared as
// the generic lowercase, which is what an admin typing the tag expects.
func foldTag(t string) string { return strings.ToLower(strings.TrimSpace(t)) }

// Load reads the schedule from app_setting, falling back to Default for a
// missing or malformed row. A database error is returned (with the default).
func Load(ctx context.Context, pool *pgxpool.Pool) (model.WorkHours, error) {
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT value FROM app_setting WHERE key=$1`, SettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	return Decode(raw), nil
}

// Decode parses a stored value; anything unusable yields Default. A row saved
// before stale_after_days existed gets the default for that field only.
func Decode(raw []byte) model.WorkHours {
	var w model.WorkHours
	if json.Unmarshal(raw, &w) != nil {
		return Default()
	}
	if w.StaleAfterDays == 0 {
		w.StaleAfterDays = defaultStaleDays
	}
	n, err := Normalize(w)
	if err != nil {
		return Default()
	}
	return n
}

// Schedule is a compiled WorkHours ready for repeated decisions.
type Schedule struct {
	days         [8]bool // ISO weekday index
	startMin     int
	endMin       int
	grace        time.Duration
	offlineAfter time.Duration
	staleDays    int
	tags         map[string]bool
}

// Compile turns a (normalized) WorkHours into a Schedule. Invalid input
// compiles the default instead, so the engine always has a usable schedule.
func Compile(w model.WorkHours) Schedule {
	n, err := Normalize(w)
	if err != nil {
		n = Default()
	}
	s := Schedule{
		grace:        time.Duration(n.GraceMinutes) * time.Minute,
		offlineAfter: time.Duration(n.OfflineAfterMinutes) * time.Minute,
		staleDays:    n.StaleAfterDays,
		tags:         map[string]bool{},
	}
	for _, d := range n.Days {
		s.days[d] = true
	}
	s.startMin, _ = parseHHMM(n.Start)
	s.endMin, _ = parseHHMM(n.End)
	for _, t := range n.Tags {
		s.tags[foldTag(t)] = true
	}
	return s
}

// Applies reports whether a machine with these tags is a work-hours-only device.
func (s Schedule) Applies(machineTags []string) bool {
	for _, t := range machineTags {
		if s.tags[foldTag(t)] {
			return true
		}
	}
	return false
}

// OfflineAfter is the minimum offline duration for work-hours devices.
func (s Schedule) OfflineAfter() time.Duration { return s.offlineAfter }

// StaleAfterDays is the number of missed work days after which a work-hours
// device counts as stale.
func (s Schedule) StaleAfterDays() int { return s.staleDays }

// MissedWorkdays counts the whole work windows that lie entirely between
// lastSeen and now — workdays on which the device was not seen at all. Counting
// stops at limit (the caller only needs to know whether a cutoff is reached), so
// a device silent for a year costs at most ~7·limit iterations.
func (s Schedule) MissedWorkdays(now, lastSeen time.Time, limit int) int {
	if limit <= 0 || !now.After(lastSeen) {
		return 0
	}
	hasDay := false
	for _, d := range s.days {
		hasDay = hasDay || d
	}
	if !hasDay {
		return 0
	}
	y, m, d := lastSeen.In(Location).Date()
	n := 0
	// Every week holds at least one workday, so limit missed days are found
	// within 7·(limit+1) calendar days of lastSeen (or now comes first).
	for i := 0; i <= 7*(limit+1); i++ {
		midnight := time.Date(y, m, d+i, 0, 0, 0, 0, Location)
		if midnight.After(now) {
			break
		}
		start, end, ok := s.Window(midnight)
		if !ok {
			continue
		}
		if end.After(now) {
			break // today's window is not over yet
		}
		if start.After(lastSeen) {
			n++
			if n >= limit {
				break
			}
		}
	}
	return n
}

// Stale reports whether a work-hours device last seen at lastSeen has missed at
// least StaleAfterDays whole work days by now.
func (s Schedule) Stale(now, lastSeen time.Time) bool {
	return s.staleDays > 0 && s.MissedWorkdays(now, lastSeen, s.staleDays) >= s.staleDays
}

// Window returns the work window of t's local (Istanbul) calendar day, and
// false when that day is not a workday.
func (s Schedule) Window(t time.Time) (start, end time.Time, ok bool) {
	lt := t.In(Location)
	wd := int(lt.Weekday())
	if wd == 0 {
		wd = 7 // ISO Sunday
	}
	if !s.days[wd] {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := lt.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, Location)
	return midnight.Add(time.Duration(s.startMin) * time.Minute),
		midnight.Add(time.Duration(s.endMin) * time.Minute), true
}

// InHours reports whether t falls inside the work window [start, end).
func (s Schedule) InHours(t time.Time) bool {
	start, end, ok := s.Window(t)
	return ok && !t.Before(start) && t.Before(end)
}

// Presence is the offline decision for one work-hours device.
type Presence struct {
	// InHours false: outside work hours — no offline alert may exist; an open
	// one is resolved at once (reason work_hours_end).
	InHours bool
	// MayOpen: a NEW alert may open now (inside the window, past the morning
	// grace, before the evening grace). An already open alert is kept while
	// InHours regardless.
	MayOpen bool
	// Age is how long the machine has been offline *within today's window*:
	// now − max(lastSeen, start). A PC off since yesterday evening counts from
	// this morning's start, not from 17:25 yesterday.
	Age time.Duration
	// NotStarted: the device has not been seen at all since today's start (it
	// was not switched on today), as opposed to going offline during the day.
	NotStarted bool
}

// OfflinePresence evaluates a work-hours device last seen at lastSeen.
func (s Schedule) OfflinePresence(now, lastSeen time.Time) Presence {
	start, end, ok := s.Window(now)
	if !ok || now.Before(start) || !now.Before(end) {
		return Presence{}
	}
	from := lastSeen
	if from.Before(start) {
		from = start
	}
	age := now.Sub(from)
	if age < 0 {
		age = 0 // clock skew: last_seen slightly in the future
	}
	mayOpen := !now.Before(start.Add(s.grace)) && now.Before(end.Add(-s.grace))
	return Presence{InHours: true, MayOpen: mayOpen, Age: age, NotStarted: lastSeen.Before(start)}
}

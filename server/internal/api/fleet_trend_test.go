package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func TestFleetTrendWindow(t *testing.T) {
	for _, tc := range []struct {
		in, key      string
		window, step time.Duration
	}{
		{"", "24h", 24 * time.Hour, 15 * time.Minute},
		{"24h", "24h", 24 * time.Hour, 15 * time.Minute},
		{"7d", "7d", 7 * 24 * time.Hour, time.Hour},
	} {
		key, w, s, ok := fleetTrendWindow(tc.in)
		if !ok || key != tc.key || w != tc.window || s != tc.step {
			t.Fatalf("%q: got %q %v %v %v", tc.in, key, w, s, ok)
		}
		// Every bucket must be a whole multiple of metric_5m's 5-minute buckets.
		if s%(5*time.Minute) != 0 {
			t.Fatalf("%q: step %v does not tile 5-minute buckets", tc.in, s)
		}
	}
	for _, bad := range []string{"30d", "1h", "7D", "x"} {
		if _, _, _, ok := fleetTrendWindow(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got := pgInterval(15 * time.Minute); got != "900 seconds" {
		t.Fatalf("pgInterval = %q", got)
	}
}

func TestFleetTrendRejectsUnknownRangeBeforeDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stats/fleet-trend?range=30d", nil)
	c := echo.New().NewContext(req, httptest.NewRecorder())
	// A zero API (nil pool) proves validation runs before any database access.
	err := (&API{}).StatsFleetTrend(c)
	wantInvalid(t, err, "range")
	if status, _ := apierr.Resolve(err); status != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", status)
	}
}

func TestSummarizeFleetTrend(t *testing.T) {
	if s := summarizeFleetTrend(nil); s != (model.FleetTrendSummary{}) {
		t.Fatalf("empty: %+v", s)
	}
	s := summarizeFleetTrend([]model.FleetTrendPoint{
		{Online: 10, Active: 4, CPUAvg: 20, MemRatioAvg: 0.5},
		{Online: 14, Active: 8, CPUAvg: 60, MemRatioAvg: 0.7},
		{Online: 12, Active: 6, CPUAvg: 10, MemRatioAvg: 0.6},
	})
	near := func(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
	if s.Online.Current != 12 || !near(s.Online.Avg, 12) || s.Online.Peak != 14 {
		t.Fatalf("online: %+v", s.Online)
	}
	if s.Active.Current != 6 || !near(s.Active.Avg, 6) || s.Active.Peak != 8 {
		t.Fatalf("active: %+v", s.Active)
	}
	if !near(s.CPUAvg.Avg, 30) || s.CPUAvg.Peak != 60 || s.CPUAvg.Current != 10 {
		t.Fatalf("cpu: %+v", s.CPUAvg)
	}
	if !near(s.MemRatioAvg.Avg, 0.6) || s.MemRatioAvg.Peak != 0.7 || s.MemRatioAvg.Current != 0.6 {
		t.Fatalf("mem: %+v", s.MemRatioAvg)
	}
}

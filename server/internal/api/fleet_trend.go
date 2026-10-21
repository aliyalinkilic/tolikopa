package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// fleetTrendWindow maps the range query value to the look-back window and the
// chart bucket width. ok is false for an unknown value ("" means the default).
// Buckets are whole multiples of metric_5m's 5-minute buckets, so re-bucketing
// never splits a source bucket (Europe/Istanbul is a whole-hour UTC offset).
func fleetTrendWindow(r string) (key string, window, step time.Duration, ok bool) {
	switch r {
	case "", "24h":
		return "24h", 24 * time.Hour, 15 * time.Minute, true
	case "7d":
		return "7d", 7 * 24 * time.Hour, time.Hour, true
	}
	return "", 0, 0, false
}

// pgInterval renders a duration as a Postgres interval literal ("900 seconds").
func pgInterval(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10) + " seconds"
}

// summarizeFleetTrend computes current (latest bucket) / mean / peak per series.
func summarizeFleetTrend(points []model.FleetTrendPoint) model.FleetTrendSummary {
	var s model.FleetTrendSummary
	if len(points) == 0 {
		return s
	}
	n := float64(len(points))
	last := points[len(points)-1]
	s.Online.Current = float64(last.Online)
	s.Active.Current = float64(last.Active)
	s.CPUAvg.Current = last.CPUAvg
	s.MemRatioAvg.Current = last.MemRatioAvg
	for _, p := range points {
		s.Online.Avg += float64(p.Online) / n
		s.Active.Avg += float64(p.Active) / n
		s.CPUAvg.Avg += p.CPUAvg / n
		s.MemRatioAvg.Avg += p.MemRatioAvg / n
		s.Online.Peak = max(s.Online.Peak, float64(p.Online))
		s.Active.Peak = max(s.Active.Peak, float64(p.Active))
		s.CPUAvg.Peak = max(s.CPUAvg.Peak, p.CPUAvg)
		s.MemRatioAvg.Peak = max(s.MemRatioAvg.Peak, p.MemRatioAvg)
	}
	return s
}

// StatsFleetTrend handles GET /api/stats/fleet-trend?range=24h|7d — fleet-wide
// online / in-use device counts and average CPU / RAM per bucket, read from the
// metric_5m continuous aggregate (real-time aggregation, so the live edge is
// included) re-bucketed in Europe/Istanbul. Archived machines are excluded.
//
//   - online: distinct machines with at least one vitals sample in the bucket.
//   - active: distinct non-Linux machines whose last session state in at least
//     one 5-minute sub-bucket was "active" (a Linux host's logind/SSH session is
//     not a person at a desk — same rule as /api/fleet/overview).
//   - cpu_avg / mem_ratio_avg: mean of the per-machine 5-minute averages.
func (a *API) StatsFleetTrend(c echo.Context) error {
	key, window, step, ok := fleetTrendWindow(c.QueryParam("range"))
	if !ok {
		return apierr.WithParams(http.StatusBadRequest, apierr.InvalidRequest,
			"range must be 24h or 7d", map[string]any{"field": "range"})
	}
	ctx := c.Request().Context()

	rows, err := a.pool.Query(ctx, `
		SELECT time_bucket($2::interval, b.bucket, 'Europe/Istanbul') AS ts,
		       count(DISTINCT b.machine_id)::int,
		       count(DISTINCT b.machine_id) FILTER (
		           WHERE b.last_state = 'active' AND m.kind IS DISTINCT FROM 'linux')::int,
		       avg(b.cpu_avg)::float8,
		       avg(b.mem_ratio_avg)::float8
		FROM metric_5m b
		JOIN machine m ON m.machine_id = b.machine_id
		WHERE b.bucket >= time_bucket($2::interval, now() - $1::interval, 'Europe/Istanbul')
		  AND m.archived_at IS NULL
		GROUP BY 1 ORDER BY 1`, pgInterval(window), pgInterval(step))
	if err != nil {
		return serverError(err)
	}
	defer rows.Close()

	resp := model.FleetTrendResponse{
		Range:         key,
		BucketSeconds: int(step / time.Second),
		GeneratedAt:   time.Now().UTC(),
		Points:        []model.FleetTrendPoint{},
	}
	for rows.Next() {
		var p model.FleetTrendPoint
		var cpu, mem *float64
		if err := rows.Scan(&p.TS, &p.Online, &p.Active, &cpu, &mem); err != nil {
			return serverError(err)
		}
		p.TS = p.TS.UTC()
		p.CPUAvg = fv(cpu)
		p.MemRatioAvg = fv(mem)
		resp.Points = append(resp.Points, p)
	}
	if err := rows.Err(); err != nil {
		return serverError(err)
	}
	resp.Summary = summarizeFleetTrend(resp.Points)
	return c.JSON(http.StatusOK, resp)
}

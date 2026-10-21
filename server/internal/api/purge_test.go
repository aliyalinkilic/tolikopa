package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
)

func TestBindMachineIDs(t *testing.T) {
	ids, err := bindMachineIDs(jsonCtx(`{"machine_ids":[" a ","b","a","",""]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("got %v, want [a b]", ids)
	}

	for _, body := range []string{`{}`, `{"machine_ids":[]}`, `{"machine_ids":["  "]}`} {
		_, err := bindMachineIDs(jsonCtx(body))
		if status, b := apierr.Resolve(err); status != http.StatusBadRequest || b.Code != apierr.InvalidRequest {
			t.Fatalf("%s: got %d %q, want 400 invalid_request", body, status, b.Code)
		}
	}
}

func TestPurgeRejectsEmptySelectionBeforeDB(t *testing.T) {
	// A zero API (nil pool) proves validation runs before any database access.
	err := (&API{}).PurgeMachines(jsonCtx(`{"machine_ids":[]}`))
	if status, _ := apierr.Resolve(err); status != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", status)
	}
}

func TestPurgeCoversEveryMachineTable(t *testing.T) {
	// Every table keyed by machine_id (see docs/DATABASE.md). A new one must be
	// added to the purge lists, or permanently deleting a machine leaves rows
	// behind — or, if it must survive the purge, to purgeRetained with a reason.
	deleted := []string{
		"metric_sample", "disk_usage_sample", "process_sample", "app_focus_sample",
		"event", "usage_session", "alert",
		"machine_live", "disk_health", "machine_remediation_state",
	}
	// Documented exclusions: remediation_attempt is the actuation audit trail
	// and outlives the machine (365-day retention).
	retained := []string{"remediation_attempt"}

	have := map[string]bool{}
	for _, n := range purgeCompressed {
		have[n] = true
	}
	for _, n := range purgePlain {
		have[n] = true
	}
	for _, tbl := range deleted {
		if !have[tbl] {
			t.Errorf("purge does not delete from %s", tbl)
		}
	}
	keep := map[string]bool{}
	for _, n := range purgeRetained {
		keep[n] = true
		if have[n] {
			t.Errorf("%s is both purged and retained", n)
		}
	}
	for _, tbl := range retained {
		if !keep[tbl] {
			t.Errorf("%s must be an explicit purge exclusion", tbl)
		}
		if have[tbl] {
			t.Errorf("purge deletes from %s, which must be kept as audit trail", tbl)
		}
	}
	if len(have)+len(keep) != len(deleted)+len(retained) {
		t.Errorf("purge lists (%d deleted, %d retained) do not match the expected tables", len(have), len(keep))
	}
}

func TestPurgeRejectsOversizedBatchBeforeDB(t *testing.T) {
	ids := make([]string, maxPurgeBatch+1)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"m%d"`, i)
	}
	err := (&API{}).PurgeMachines(jsonCtx(`{"machine_ids":[` + strings.Join(ids, ",") + `]}`))
	status, b := apierr.Resolve(err)
	if status != http.StatusBadRequest || b.Code != "too_many_items" {
		t.Fatalf("got %d %q, want 400 too_many_items", status, b.Code)
	}
}

func TestPurgeRefreshesAreSerialisedAndCoalesced(t *testing.T) {
	var (
		mu       sync.Mutex
		active   int
		overlap  bool
		calls    []map[string]time.Time
		release  = make(chan struct{})
		started  = make(chan struct{}, 8)
		finished = make(chan struct{}, 8)
	)
	r := &purgeRefresher{run: func(_ execer, since map[string]time.Time) {
		mu.Lock()
		active++
		if active > 1 {
			overlap = true
		}
		calls = append(calls, since)
		mu.Unlock()
		started <- struct{}{}
		<-release
		mu.Lock()
		active--
		mu.Unlock()
		finished <- struct{}{}
	}}

	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	r.schedule(nil, map[string]time.Time{"metric_sample": t0.Add(3 * time.Hour)})
	<-started // first pass is running
	// Two purges commit while it runs: they must merge into one pending pass.
	r.schedule(nil, map[string]time.Time{"metric_sample": t0.Add(time.Hour)})
	r.schedule(nil, map[string]time.Time{"metric_sample": t0.Add(2 * time.Hour), "process_sample": t0})
	r.schedule(nil, nil) // nothing to refresh: no-op
	close(release)
	<-finished
	<-started
	<-finished

	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		idle := !r.running
		r.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh loop did not stop")
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if overlap {
		t.Fatal("refreshes overlapped")
	}
	if len(calls) != 2 {
		t.Fatalf("got %d refresh passes, want 2", len(calls))
	}
	if got := calls[1]["metric_sample"]; !got.Equal(t0.Add(time.Hour)) {
		t.Errorf("coalesced metric_sample start = %v, want earliest %v", got, t0.Add(time.Hour))
	}
	if got := calls[1]["process_sample"]; !got.Equal(t0) {
		t.Errorf("coalesced process_sample start = %v, want %v", got, t0)
	}
}

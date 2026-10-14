package sender

import (
	"compress/gzip"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// A 413 must not discard the whole batch: the agent splits it and delivers
// everything the server will take.
func TestServer413SplitsInsteadOfDroppingAll(t *testing.T) {
	var mu sync.Mutex
	gotSamples, gotEvents, rejected := 0, 0, 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		b, _ := io.ReadAll(zr)
		var env model.Envelope
		if json.Unmarshal(b, &env) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		// Pretend the server's app_focus cap is far below the agent's.
		if len(env.AppFocus) > 1 {
			rejected++
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		gotSamples += len(env.Samples)
		gotEvents += len(env.Events)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.BackendURL = srv.URL
	cfg.TLSCAPem = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	s, err := New(cfg, model.Machine{MachineID: "m"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.spoolDir = t.TempDir()

	for i := 0; i < 3; i++ {
		s.AddSample(model.Sample{})
	}
	s.AddEvents(make([]model.Event, 4))
	for _, n := range []string{"a", "b", "c", "d"} {
		s.AddAppFocus(n, 5)
	}
	if err := s.sendNow(); err != nil {
		t.Fatalf("sendNow: %v", err)
	}
	if gotSamples != 3 || gotEvents != 4 {
		t.Fatalf("delivered samples=%d events=%d, want 3/4 (rejections=%d)", gotSamples, gotEvents, rejected)
	}
	if rejected == 0 {
		t.Fatal("test server never rejected; test is not exercising the 413 path")
	}
}

func TestNewRefusesHTTP(t *testing.T) {
	cfg := config.Default()
	cfg.BackendURL = "http://10.0.0.10"
	if _, err := New(cfg, model.Machine{}, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("sender accepted an http:// backend_url")
	}
}

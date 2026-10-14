// Package sender batches samples/events/health into model.Envelope values and
// delivers them to the ingest endpoint, spooling to disk on failure.
package sender

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

const (
	maxBatchSamples = 200 // flush early at N items (server hard cap is 240)
	httpTimeout     = 30 * time.Second
	backoffBase     = 30 * time.Second
	backoffCap      = 15 * time.Minute
)

// Sender accumulates telemetry and POSTs it as gzip JSON.
type Sender struct {
	cfg      *config.Config
	machine  model.Machine
	log      *slog.Logger
	client   *http.Client
	url      string
	spoolDir string

	mu          sync.Mutex
	samples     []model.Sample
	events      []model.Event
	health      []model.DiskHealth
	updateSts   *model.UpdateStatus      // latest Windows Update posture (latest wins)
	battery     *model.Battery           // latest battery/power state (latest wins)
	appFocus    map[string]int           // app base name -> accumulated foreground seconds
	remediation *model.RemediationReport // latest hygiene posture (latest wins)
	onPolicy    func(*model.RemediationPolicy)

	trigger chan struct{}

	failCount   int
	nextAttempt time.Time
}

// New builds a Sender, pinning the configured CA if tls_ca_pem is set.
func New(cfg *config.Config, machine model.Machine, log *slog.Logger) (*Sender, error) {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if strings.TrimSpace(cfg.TLSCAPem) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(cfg.TLSCAPem)) {
			return nil, fmt.Errorf("tls_ca_pem: no certificates parsed")
		}
		// Pin the internal CA. InsecureSkipVerify is NEVER set.
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &Sender{
		cfg:      cfg,
		machine:  machine,
		log:      log,
		client:   &http.Client{Timeout: httpTimeout, Transport: tr},
		url:      strings.TrimRight(cfg.BackendURL, "/") + "/api/v1/ingest",
		spoolDir: config.SpoolDir(),
		trigger:  make(chan struct{}, 1),
	}, nil
}

// AddSample buffers one sample, triggering an early flush at the batch cap.
func (s *Sender) AddSample(sm model.Sample) {
	s.mu.Lock()
	s.samples = append(s.samples, sm)
	n := len(s.samples)
	s.mu.Unlock()
	if n >= maxBatchSamples {
		s.signal()
	}
}

// AddEvents buffers crash/hang events.
func (s *Sender) AddEvents(ev []model.Event) {
	if len(ev) == 0 {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, ev...)
	s.mu.Unlock()
}

// AddHealth buffers the latest SMART snapshot.
func (s *Sender) AddHealth(h []model.DiskHealth) {
	if len(h) == 0 {
		return
	}
	s.mu.Lock()
	s.health = append(s.health, h...)
	s.mu.Unlock()
}

// SetUpdateStatus stores the latest Windows Update posture (latest wins). It is
// attached to the next envelope and cleared once sent. A nil value is ignored.
func (s *Sender) SetUpdateStatus(u *model.UpdateStatus) {
	if u == nil {
		return
	}
	s.mu.Lock()
	s.updateSts = u
	s.mu.Unlock()
}

// SetBattery stores the latest battery/power state (latest wins).
func (s *Sender) SetBattery(b *model.Battery) {
	if b == nil {
		return
	}
	s.mu.Lock()
	s.battery = b
	s.mu.Unlock()
}

// SetRemediation stores the latest endpoint-hygiene report (latest wins). It is
// attached to the next envelope and cleared once sent. A nil value is ignored.
func (s *Sender) SetRemediation(r *model.RemediationReport) {
	if r == nil {
		return
	}
	s.mu.Lock()
	s.remediation = r
	s.mu.Unlock()
}

// SetPolicyHandler registers the callback that receives a remediation policy the
// server returns in a 202 response. It is invoked ONLY for live deliveries: a
// spooled replay may be hours old and must never trigger work (see drainSpool).
// The callback runs on the sender goroutine, so it must be quick and must not
// block on the network.
func (s *Sender) SetPolicyHandler(f func(*model.RemediationPolicy)) {
	s.mu.Lock()
	s.onPolicy = f
	s.mu.Unlock()
}

// AddAppFocus accumulates foreground (active-use) seconds for one application,
// keyed by its base NAME only. Flushed with the next envelope.
func (s *Sender) AddAppFocus(name string, seconds int) {
	if name == "" || seconds <= 0 {
		return
	}
	s.mu.Lock()
	if s.appFocus == nil {
		s.appFocus = map[string]int{}
	}
	s.appFocus[name] += seconds
	s.mu.Unlock()
}

func (s *Sender) signal() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Run flushes every post_sec (heartbeat even when idle) and on the early-flush
// trigger, applying exponential backoff to network/5xx failures. It returns
// after a final flush when ctx is cancelled.
func (s *Sender) Run(ctx context.Context) {
	interval := time.Duration(s.cfg.Intervals.PostSec) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.finalFlush()
			return
		case <-ticker.C:
			s.tick(false)
		case <-s.trigger:
			s.tick(true)
		}
	}
}

// tick attempts delivery unless in a backoff window, in which case it only
// spills buffered data to the spool to bound memory.
func (s *Sender) tick(early bool) {
	if time.Now().Before(s.nextAttempt) {
		if early {
			s.spill(s.swap())
		}
		return
	}
	if err := s.sendNow(); err != nil {
		s.failCount++
		d := backoffFor(s.failCount)
		s.nextAttempt = time.Now().Add(d)
		s.log.Warn("ingest delivery failed, backing off", "err", err.Error(), "retry_in", d.String())
	} else {
		s.failCount = 0
		s.nextAttempt = time.Time{}
	}
}

// sendNow drains the spool, then delivers the current buffer (heartbeat if empty).
func (s *Sender) sendNow() error {
	if err := s.drainSpool(); err != nil {
		// Server/network still down: preserve current buffer and keep backing off.
		s.spill(s.swap())
		return err
	}

	samples, events, health, upd, focus, battery, rem := s.swap()
	env := s.envelope(samples, events, health, upd, focus, battery, rem)
	body, err := json.Marshal(env)
	if err != nil {
		s.log.Error("marshal envelope", "err", err.Error())
		return nil // unrecoverable for this batch; drop
	}

	// applyPolicy=true: this is the LIVE path, so a directive in the response is
	// fresh and may be acted on.
	status, pol, perr := s.post(env.BatchID, body, true)
	switch {
	case perr != nil:
		if hasContent(samples, events, health, upd, focus, battery, rem) {
			s.writeSpool(env.BatchID, body)
		}
		return perr
	case status >= 200 && status < 300:
		s.deliverPolicy(pol)
		return nil
	case status == 401 || status == 403 || (status >= 400 && status < 500 && status != 429):
		// Bad token / bad request: stop retrying this batch.
		s.log.Error("ingest rejected (4xx), dropping batch", "status", status)
		return nil
	default: // 429 / 5xx
		if hasContent(samples, events, health, upd, focus, battery, rem) {
			s.writeSpool(env.BatchID, body)
		}
		return fmt.Errorf("ingest status %d", status)
	}
}

// deliverPolicy hands a freshly received policy to the registered handler. The
// handler validates it against the compiled catalog and persists it; it is never
// called from the spool replay path.
func (s *Sender) deliverPolicy(pol *model.RemediationPolicy) {
	if pol == nil {
		return
	}
	s.mu.Lock()
	h := s.onPolicy
	s.mu.Unlock()
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("policy handler panicked", "recover", fmt.Sprint(r))
		}
	}()
	h(pol)
}

// finalFlush persists the current buffer to the spool first (so a slow or
// hung shutdown send never loses data) then makes a best-effort delivery. A
// later successful resend is de-duplicated by the server via Idempotency-Key.
func (s *Sender) finalFlush() {
	s.spill(s.swap())
	if err := s.drainSpool(); err != nil {
		s.log.Warn("final flush left data spooled", "err", err.Error())
	}
}

// swap atomically takes ownership of the current buffers.
func (s *Sender) swap() (samples []model.Sample, events []model.Event, health []model.DiskHealth, upd *model.UpdateStatus, focus []model.AppFocus, battery *model.Battery, rem *model.RemediationReport) {
	s.mu.Lock()
	samples, events, health, upd, battery, rem = s.samples, s.events, s.health, s.updateSts, s.battery, s.remediation
	for name, sec := range s.appFocus {
		focus = append(focus, model.AppFocus{Name: name, Seconds: sec})
	}
	s.samples, s.events, s.health, s.updateSts, s.battery, s.appFocus, s.remediation = nil, nil, nil, nil, nil, nil, nil
	s.mu.Unlock()
	return
}

// spill writes buffers to the spool (used during backoff to bound memory).
func (s *Sender) spill(samples []model.Sample, events []model.Event, health []model.DiskHealth, upd *model.UpdateStatus, focus []model.AppFocus, battery *model.Battery, rem *model.RemediationReport) {
	if !hasContent(samples, events, health, upd, focus, battery, rem) {
		return
	}
	env := s.envelope(samples, events, health, upd, focus, battery, rem)
	if body, err := json.Marshal(env); err == nil {
		s.writeSpool(env.BatchID, body)
	}
}

func (s *Sender) envelope(samples []model.Sample, events []model.Event, health []model.DiskHealth, upd *model.UpdateStatus, focus []model.AppFocus, battery *model.Battery, rem *model.RemediationReport) model.Envelope {
	// The machine block is static identity captured at startup, except for the
	// LAN address, which DHCP and Wi-Fi/Ethernet switches change at will — so it
	// is re-read per batch (a handful of syscalls; cheaper than one sample).
	machine := s.machine
	machine.IPAddrs = collector.LocalIPv4s()
	return model.Envelope{
		BatchID:      uuid.NewString(),
		Machine:      machine,
		Samples:      samples,
		Events:       events,
		DiskHealth:   health,
		UpdateStatus: upd,
		AppFocus:     focus,
		Battery:      battery,
		Remediation:  rem,
		SentAt:       time.Now().UTC(),
	}
}

// post delivers one envelope body as gzip JSON. batchID is reused as the
// Idempotency-Key so retries (including spooled ones) are idempotent.
//
// applyPolicy selects whether the 202 body is parsed for a remediation policy.
// Only the LIVE path passes true; the spool replay must not (see drainSpool).
// A body that does not decode is NOT a batch failure: older servers answer with a
// different shape, and the delivery itself still succeeded.
func (s *Sender) post(batchID string, body []byte, applyPolicy bool) (int, *model.RemediationPolicy, error) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(body); err != nil {
		return 0, nil, err
	}
	if err := zw.Close(); err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequest(http.MethodPost, s.url, &gz)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+s.cfg.IngestToken)
	req.Header.Set("Idempotency-Key", batchID)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var policy *model.RemediationPolicy
	if applyPolicy && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var ir model.IngestResponse
		if derr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ir); derr != nil {
			s.log.Debug("ingest response not decodable", "err", derr.Error())
		} else {
			policy = ir.Policy
		}
	}
	// Drain whatever is left so the connection can be reused.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, policy, nil
}

func hasContent(samples []model.Sample, events []model.Event, health []model.DiskHealth, upd *model.UpdateStatus, focus []model.AppFocus, battery *model.Battery, rem *model.RemediationReport) bool {
	return len(samples) > 0 || len(events) > 0 || len(health) > 0 || upd != nil || len(focus) > 0 || battery != nil || rem != nil
}

func backoffFor(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	shift := fails - 1
	if shift > 5 {
		shift = 5
	}
	d := backoffBase << shift
	if d > backoffCap {
		d = backoffCap
	}
	return d
}

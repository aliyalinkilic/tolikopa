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

	// Touched only by the Run goroutine (tick/sendNow/drainSpool/finalFlush).
	failCount   int
	nextAttempt time.Time
	// rejectLogged throttles the "server rejected a batch" log per HTTP status,
	// so a persistent 4xx logs once per rejectLogEvery instead of every minute.
	rejectLogged map[int]time.Time
}

// rejectLogEvery is the minimum gap between two identical 4xx rejection logs.
const rejectLogEvery = time.Hour

// New builds a Sender, pinning the configured CA if tls_ca_pem is set.
func New(cfg *config.Config, machine model.Machine, log *slog.Logger) (*Sender, error) {
	// Load already enforces this; re-checked here so no construction path can
	// ever put the bearer token on a cleartext connection.
	if err := config.ValidateBackendURL(cfg.BackendURL); err != nil {
		return nil, err
	}
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
		cfg:     cfg,
		machine: machine,
		log:     log,
		client: &http.Client{
			Timeout:   httpTimeout,
			Transport: tr,
			// net/http keeps the Authorization header across a same-host redirect,
			// even to http://. Never follow a redirect off TLS.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("refusing non-https redirect")
				}
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		url:      strings.TrimRight(strings.TrimSpace(cfg.BackendURL), "/") + "/api/v1/ingest",
		spoolDir: config.SpoolDir(),
		trigger:  make(chan struct{}, 1),
	}, nil
}

// AddSample buffers one sample, triggering an early flush at the batch cap.
// The buffer is bounded (drop-oldest) in case the sender goroutine is busy.
func (s *Sender) AddSample(sm model.Sample) {
	s.mu.Lock()
	s.samples = append(s.samples, sm)
	if over := len(s.samples) - maxBufferedSamples; over > 0 {
		s.samples = append(s.samples[:0:0], s.samples[over:]...)
	}
	n := len(s.samples)
	s.mu.Unlock()
	if n >= maxBatchSamples {
		s.signal()
	}
}

// AddEvents buffers crash/hang events. The buffer is bounded: once full, newer
// events are dropped (a crash storm is already visible from what was kept).
func (s *Sender) AddEvents(ev []model.Event) {
	if len(ev) == 0 {
		return
	}
	s.mu.Lock()
	if room := maxBufferedEvents - len(s.events); room > 0 {
		if len(ev) > room {
			ev = ev[:room]
		}
		s.events = append(s.events, ev...)
	}
	s.mu.Unlock()
}

// AddHealth buffers the latest SMART snapshot (bounded, newest kept).
func (s *Sender) AddHealth(h []model.DiskHealth) {
	if len(h) == 0 {
		return
	}
	s.mu.Lock()
	s.health = append(s.health, h...)
	if over := len(s.health) - maxBufferedHealth; over > 0 {
		s.health = append(s.health[:0:0], s.health[over:]...)
	}
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
	r = capRemediation(r)
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

// maxFocusSeconds caps one app's accumulated seconds between flushes; far above
// any real value (a flush interval is minutes), it only rules out int overflow
// from a hostile helper.
const maxFocusSeconds = 7 * 24 * 3600

// AddAppFocus accumulates foreground (active-use) seconds for one application,
// keyed by its base NAME only. Flushed with the next envelope. Names come from
// the user-context helper and are re-validated here; the accumulator is bounded
// (a new name is ignored once maxTrackedApps distinct names are held), and the
// envelope later keeps only the server's row cap, by most seconds.
func (s *Sender) AddAppFocus(name string, seconds int) {
	n, ok := collector.ValidAppName(name)
	if !ok || seconds <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appFocus == nil {
		s.appFocus = map[string]int{}
	}
	cur, seen := s.appFocus[n]
	if !seen && len(s.appFocus) >= maxTrackedApps {
		return
	}
	s.appFocus[n] = min(cur+min(seconds, maxFocusSeconds), maxFocusSeconds)
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

	// The buffer may hold more than one envelope's worth (e.g. after a long spool
	// drain); chunk it so no envelope can exceed a server cap.
	chunks := chunk(s.swap())
	for i, p := range chunks {
		// applyPolicy=true: this is the LIVE path, so a directive in the
		// response is fresh and may be acted on.
		if err := s.sendParts(p, true, 0); err != nil {
			for _, rest := range chunks[i+1:] {
				s.spillParts(rest, 0)
			}
			return err
		}
	}
	return nil
}

// sendParts delivers one envelope's content. A body over maxBodyBytes, or one
// the server answers 413 for, is split and the halves delivered separately, so
// one oversized part can never take the rest of the batch down with it. On a
// retryable failure (network / 429 / 5xx) the undelivered content is spooled and
// the error returned so the caller backs off.
func (s *Sender) sendParts(p parts, applyPolicy bool, depth int) error {
	env := s.envelope(p)
	body, err := json.Marshal(env)
	if err != nil {
		s.log.Error("marshal envelope", "err", err.Error())
		return nil // unrecoverable for this batch; drop
	}
	if len(body) > maxBodyBytes {
		return s.sendSplit(p, applyPolicy, depth)
	}
	return s.sendBody(env.BatchID, body, p, applyPolicy, depth)
}

// sendBody posts an already-marshalled envelope whose content is p.
func (s *Sender) sendBody(batchID string, body []byte, p parts, applyPolicy bool, depth int) error {
	status, pol, perr := s.post(batchID, body, applyPolicy)
	switch {
	case perr != nil:
		if !p.empty() {
			s.writeSpool(batchID, body)
		}
		return perr
	case status >= 200 && status < 300:
		if applyPolicy {
			s.deliverPolicy(pol)
		}
		return nil
	case status == http.StatusRequestEntityTooLarge:
		// The agent's own caps should make this unreachable; if a server with
		// tighter caps answers it anyway, deliver what fits rather than dropping
		// metrics, events and audit wholesale every minute.
		s.logReject(status, "ingest rejected batch as too large (413), splitting")
		return s.sendSplit(p, applyPolicy, depth)
	case status >= 400 && status < 500 && status != http.StatusTooManyRequests:
		// Bad token / bad request: retrying this batch cannot help; drop it.
		s.logReject(status, "ingest rejected (4xx), dropping batch")
		return nil
	default: // 429 / 5xx
		if !p.empty() {
			s.writeSpool(batchID, body)
		}
		return fmt.Errorf("ingest status %d", status)
	}
}

// sendSplit delivers p as two (or more, recursively) smaller batches, each with
// a fresh batch id. Content that cannot be divided further is dropped and logged.
func (s *Sender) sendSplit(p parts, applyPolicy bool, depth int) error {
	halves := p.split()
	if depth >= maxSplitDepth || halves == nil {
		s.log.Warn("dropping batch the server will not accept even after splitting",
			"samples", len(p.samples), "events", len(p.events), "app_focus", len(p.focus))
		return nil
	}
	for i, h := range halves {
		if err := s.sendParts(h, applyPolicy, depth+1); err != nil {
			for _, rest := range halves[i+1:] {
				s.spillParts(rest, depth+1)
			}
			return err
		}
	}
	return nil
}

// logReject logs a 4xx rejection at Error at most once per rejectLogEvery per
// status code (Debug otherwise), so a persistent rejection is visible without
// flooding the log every post interval.
func (s *Sender) logReject(status int, msg string) {
	now := time.Now()
	if s.rejectLogged == nil {
		s.rejectLogged = map[int]time.Time{}
	}
	if last, ok := s.rejectLogged[status]; ok && now.Sub(last) < rejectLogEvery {
		s.log.Debug(msg, "status", status)
		return
	}
	s.rejectLogged[status] = now
	s.log.Error(msg, "status", status)
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

// swap atomically takes ownership of the current buffers. app_focus is reduced
// to the server's row cap here, keeping the apps with the most seconds.
func (s *Sender) swap() parts {
	s.mu.Lock()
	p := parts{
		samples: s.samples,
		events:  s.events,
		health:  s.health,
		upd:     s.updateSts,
		focus:   topFocus(s.appFocus),
		battery: s.battery,
		rem:     s.remediation,
	}
	s.samples, s.events, s.health, s.updateSts, s.battery, s.appFocus, s.remediation = nil, nil, nil, nil, nil, nil, nil
	s.mu.Unlock()
	return p
}

// spill writes buffers to the spool (used during backoff to bound memory),
// chunked exactly like a live send so a replay can never exceed a server cap.
func (s *Sender) spill(p parts) {
	for _, c := range chunk(p) {
		s.spillParts(c, 0)
	}
}

// spillParts spools one envelope's content, splitting it if it marshals over
// maxBodyBytes.
func (s *Sender) spillParts(p parts, depth int) {
	if p.empty() {
		return
	}
	env := s.envelope(p)
	body, err := json.Marshal(env)
	if err != nil {
		return
	}
	if len(body) > maxBodyBytes {
		if halves := p.split(); halves != nil && depth < maxSplitDepth {
			for _, h := range halves {
				s.spillParts(h, depth+1)
			}
			return
		}
		s.log.Warn("dropping oversize batch instead of spooling it", "bytes", len(body))
		return
	}
	s.writeSpool(env.BatchID, body)
}

func (s *Sender) envelope(p parts) model.Envelope {
	var machine model.Machine
	if p.machine != nil {
		// Re-split spooled content keeps the identity it was collected under.
		machine = *p.machine
	} else {
		// The machine block is static identity captured at startup, except for
		// the LAN address, which DHCP and Wi-Fi/Ethernet switches change at will
		// — so it is re-read per batch (a handful of syscalls; cheaper than one
		// sample).
		machine = s.machine
		machine.IPAddrs = collector.LocalIPv4s()
	}
	return model.Envelope{
		BatchID:      uuid.NewString(),
		Machine:      machine,
		Samples:      p.samples,
		Events:       p.events,
		DiskHealth:   p.health,
		UpdateStatus: p.upd,
		AppFocus:     p.focus,
		Battery:      p.battery,
		Remediation:  p.rem,
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

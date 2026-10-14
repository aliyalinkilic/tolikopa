package sender

import (
	"sort"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

// Server-side hard caps (server/internal/ingest/ingest.go, docs/API.md §1). The
// server answers 413 for the WHOLE batch when any of these is exceeded, which
// would discard metrics, crash events and the remediation audit together. The
// agent therefore never builds an envelope that can exceed them. Keep these in
// sync with the server by hand, like the model structs.
const (
	srvMaxDecompressed        = 1 << 20 // decompressed JSON body
	srvMaxSamples             = 240
	srvMaxDisksPerSample      = 20
	srvMaxProcessRows         = 5000 // summed over all samples of one envelope
	srvMaxAppFocusRows        = 200
	srvMaxRemediationItems    = 512
	srvMaxRemediationAttempts = 512
)

// Agent-side envelope and buffer budgets (all strictly inside the server caps).
const (
	// maxBodyBytes leaves headroom under srvMaxDecompressed for the machine
	// block and JSON framing differences.
	maxBodyBytes = srvMaxDecompressed - 96<<10
	// maxEnvelopeEvents bounds events per envelope (the server has no row cap,
	// but the body-size cap applies); more events go into further envelopes.
	maxEnvelopeEvents = 500
	// Memory bounds for data buffered between flushes (e.g. while the sender
	// goroutine is busy draining a long spool).
	maxBufferedSamples = 2000
	maxBufferedEvents  = 5000
	maxBufferedHealth  = 64
	// maxTrackedApps bounds the app_focus accumulator; the envelope carries only
	// the srvMaxAppFocusRows entries with the most seconds.
	maxTrackedApps = 1000
	// maxSplitDepth bounds recursive halving of a batch the server refuses as too
	// large (2^6 = at most 64 sub-batches from one batch).
	maxSplitDepth = 6
)

// parts is the content of one envelope, before the envelope wrapper is built.
// machine is nil for live data (the envelope gets the current identity) and set
// for a spooled envelope being re-split, which must keep its original identity
// (boot_time drives the server's NIC counter-reset handling).
type parts struct {
	machine *model.Machine
	samples []model.Sample
	events  []model.Event
	health  []model.DiskHealth
	upd     *model.UpdateStatus
	focus   []model.AppFocus
	battery *model.Battery
	rem     *model.RemediationReport
}

func (p parts) hasAux() bool {
	return len(p.events) > 0 || len(p.health) > 0 || p.upd != nil || len(p.focus) > 0 || p.battery != nil || p.rem != nil
}

func (p parts) empty() bool { return len(p.samples) == 0 && !p.hasAux() }

func partsOf(env model.Envelope) parts {
	m := env.Machine
	return parts{
		machine: &m,
		samples: env.Samples,
		events:  env.Events,
		health:  env.DiskHealth,
		upd:     env.UpdateStatus,
		focus:   env.AppFocus,
		battery: env.Battery,
		rem:     env.Remediation,
	}
}

// topFocus converts the accumulator to rows, keeping the srvMaxAppFocusRows
// entries with the most seconds (name breaks ties, for determinism).
func topFocus(acc map[string]int) []model.AppFocus {
	if len(acc) == 0 {
		return nil
	}
	out := make([]model.AppFocus, 0, len(acc))
	for name, sec := range acc {
		out = append(out, model.AppFocus{Name: name, Seconds: sec})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > srvMaxAppFocusRows {
		out = out[:srvMaxAppFocusRows]
	}
	return out
}

// capRemediation returns a shallow copy of r with its lists truncated to the
// server's caps. The catalog is far smaller than the caps today; this only
// guarantees a future catalog growth can never 413 every batch.
func capRemediation(r *model.RemediationReport) *model.RemediationReport {
	c := *r
	if len(c.Present) > srvMaxRemediationItems {
		c.Present = c.Present[:srvMaxRemediationItems]
	}
	if len(c.Attempts) > srvMaxRemediationAttempts {
		c.Attempts = c.Attempts[:srvMaxRemediationAttempts]
	}
	return &c
}

// capSample truncates one sample's per-sample lists to the server's caps.
func capSample(s model.Sample) model.Sample {
	if len(s.Disks) > srvMaxDisksPerSample {
		s.Disks = s.Disks[:srvMaxDisksPerSample]
	}
	if len(s.Processes) > srvMaxProcessRows {
		s.Processes = s.Processes[:srvMaxProcessRows]
	}
	return s
}

// chunk splits buffered data into envelopes that each respect the row caps:
// ≤ maxBatchSamples samples, ≤ srvMaxProcessRows process rows, ≤
// maxEnvelopeEvents events. The single-valued and small fields (health, update,
// focus, battery, remediation) ride on the first envelope. Always returns at
// least one element (an empty one is a heartbeat).
func chunk(p parts) []parts {
	if p.rem != nil {
		p.rem = capRemediation(p.rem)
	}
	if len(p.focus) > srvMaxAppFocusRows {
		p.focus = p.focus[:srvMaxAppFocusRows]
	}
	first := parts{machine: p.machine, health: p.health, upd: p.upd, focus: p.focus, battery: p.battery, rem: p.rem}
	out := []parts{first}
	cur := &out[0]
	procRows := 0
	for _, s := range p.samples {
		s = capSample(s)
		if len(cur.samples) >= maxBatchSamples || procRows+len(s.Processes) > srvMaxProcessRows {
			out = append(out, parts{machine: p.machine})
			cur = &out[len(out)-1]
			procRows = 0
		}
		cur.samples = append(cur.samples, s)
		procRows += len(s.Processes)
	}
	// Events fill envelopes in order, starting with the first.
	ev := p.events
	for i := 0; len(ev) > 0; i++ {
		if i == len(out) {
			out = append(out, parts{machine: p.machine})
		}
		n := min(len(ev), maxEnvelopeEvents)
		out[i].events = ev[:n]
		ev = ev[n:]
	}
	return out
}

// split halves a batch the server refused as too large (or that marshals over
// maxBodyBytes). It peels off the cheapest-to-separate part first: telemetry
// samples from the auxiliary data, then halves of the largest list. It returns
// nil when the batch cannot be divided further.
func (p parts) split() []parts {
	switch {
	case len(p.samples) > 0 && p.hasAux():
		aux := p
		aux.samples = nil
		return []parts{{machine: p.machine, samples: p.samples}, aux}
	case len(p.samples) > 1:
		h := len(p.samples) / 2
		return []parts{{machine: p.machine, samples: p.samples[:h]}, {machine: p.machine, samples: p.samples[h:]}}
	case len(p.events) > 1:
		h := len(p.events) / 2
		a, b := p, parts{machine: p.machine}
		a.events, b.events = p.events[:h], p.events[h:]
		return []parts{a, b}
	case len(p.focus) > 1:
		h := len(p.focus) / 2
		a, b := p, parts{machine: p.machine}
		a.focus, b.focus = p.focus[:h], p.focus[h:]
		return []parts{a, b}
	}
	return nil
}

package sender

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/model"
)

const spoolExt = ".ndjson"

// writeSpool persists one envelope body as a single NDJSON line, then enforces
// the spool size cap (drop-oldest).
func (s *Sender) writeSpool(batchID string, body []byte) {
	if err := os.MkdirAll(s.spoolDir, 0o755); err != nil {
		s.log.Error("spool mkdir", "err", err.Error())
		return
	}
	name := fmt.Sprintf("%020d-%s%s", time.Now().UnixNano(), batchID, spoolExt)
	path := filepath.Join(s.spoolDir, name)
	line := append(append([]byte{}, body...), '\n')
	if err := os.WriteFile(path, line, 0o640); err != nil {
		s.log.Error("spool write", "err", err.Error())
		return
	}
	s.enforceSpoolCap()
}

// spoolFiles returns the spool file paths sorted oldest-first (name is time-prefixed).
func (s *Sender) spoolFiles() []string {
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), spoolExt) {
			files = append(files, filepath.Join(s.spoolDir, e.Name()))
		}
	}
	sort.Strings(files)
	return files
}

// enforceSpoolCap drops the oldest spool files until the directory is within
// spool_max_mb.
func (s *Sender) enforceSpoolCap() {
	capBytes := int64(s.cfg.SpoolMaxMB) * 1024 * 1024
	if capBytes <= 0 {
		return
	}
	files := s.spoolFiles()
	var total int64
	sizes := make(map[string]int64, len(files))
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			sizes[f] = fi.Size()
			total += fi.Size()
		}
	}
	for _, f := range files { // oldest first
		if total <= capBytes {
			break
		}
		if err := os.Remove(f); err == nil {
			total -= sizes[f]
			s.log.Warn("spool over cap, dropped oldest batch", "file", filepath.Base(f))
		}
	}
}

// drainSpool attempts to deliver spooled batches oldest-first. It stops at the
// first retryable (network/5xx) failure, returning that error so the caller can
// back off; delivered and 4xx-rejected batches are removed.
func (s *Sender) drainSpool() error {
	for _, f := range s.spoolFiles() {
		b, err := os.ReadFile(f)
		if err != nil {
			os.Remove(f)
			continue
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		var remaining []string
		for i, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			var env model.Envelope
			if jerr := json.Unmarshal([]byte(ln), &env); jerr != nil {
				continue // corrupt line: drop
			}
			// applyPolicy=false, and the returned policy is discarded ON PURPOSE.
			// A spooled batch can be hours or days old (the machine was offline,
			// or the server was down); the policy the server attaches to its
			// answer is a reply to THAT stale report. Acting on it here would let
			// a replay actuate removals with an out-of-date view of the machine —
			// and would do so N times, once per spooled line. Fresh directives
			// only ever arrive on the live path in sendNow.
			status, _, perr := s.post(env.BatchID, []byte(ln), false)
			switch {
			case perr != nil:
				remaining = append(remaining, lines[i:]...)
				s.rewriteSpool(f, remaining)
				return perr
			case status >= 200 && status < 300:
				// delivered
			case status == 429 || status >= 500:
				remaining = append(remaining, lines[i:]...)
				s.rewriteSpool(f, remaining)
				return fmt.Errorf("spool resend status %d", status)
			case status == http.StatusRequestEntityTooLarge:
				// Typically a batch spooled by an older build without the agent-side
				// caps. Re-split it (fresh batch ids, original machine identity,
				// still applyPolicy=false) instead of discarding all of it.
				s.logReject(status, "spooled batch rejected as too large (413), splitting")
				if serr := s.sendSplit(partsOf(env), false, 0); serr != nil {
					// Undelivered halves were spooled as new files by sendSplit.
					remaining = append(remaining, lines[i+1:]...)
					s.rewriteSpool(f, remaining)
					return serr
				}
			default: // 4xx: poison, drop
				s.logReject(status, "spooled batch rejected (4xx), dropping")
			}
		}
		os.Remove(f) // all lines consumed
	}
	return nil
}

// rewriteSpool replaces a spool file with the still-undelivered lines.
func (s *Sender) rewriteSpool(path string, lines []string) {
	if len(lines) == 0 {
		os.Remove(path)
		return
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		s.log.Error("spool rewrite", "err", err.Error())
	}
}

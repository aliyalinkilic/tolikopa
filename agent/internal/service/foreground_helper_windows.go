//go:build windows

package service

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
)

const (
	fgPollInterval   = 4 * time.Second
	fgReportInterval = 30 * time.Second
	fgIdleThreshold  = 90 // seconds of no input above which the user is "away"
)

// RunForegroundHelper is the `--fg-helper` process mode. It runs INSIDE the
// interactive user's session (launched by the SYSTEM agent) and, while the user
// is actively using the machine, tallies foreground time per application BASE
// NAME — never the window title, document, tab, or URL. Every fgReportInterval it
// writes the accumulated {name: seconds} tallies as one JSON line to stdout, which
// the parent agent reads over an inherited pipe.
//
// Protocol: keys prefixed with "__" are reserved control fields, never app names.
// Every line carries "__idle": seconds since the last keyboard/mouse input
// (GetLastInputInfo recency — input-presence timing only, never key contents). A
// line is emitted on EVERY report tick, even with no app focus to report (e.g.
// {"__idle":420}), so the parent's idle data stays fresh. The helper exits when
// the pipe breaks (agent gone) or the process is terminated (session ended).
func RunForegroundHelper() error {
	w := bufio.NewWriter(os.Stdout)
	acc := map[string]int{}
	poll := time.NewTicker(fgPollInterval)
	defer poll.Stop()
	report := time.NewTicker(fgReportInterval)
	defer report.Stop()

	step := int(fgPollInterval / time.Second)
	for {
		select {
		case <-poll.C:
			if collector.IdleSeconds() > fgIdleThreshold {
				continue // user away — do not attribute time to any app
			}
			if name, ok := collector.ForegroundAppName(); ok && !strings.HasPrefix(name, "__") {
				acc[name] += step // "__" is reserved for control fields
			}
		case <-report.C:
			out := make(map[string]int, len(acc)+1)
			for name, sec := range acc {
				out[name] = sec
			}
			out["__idle"] = collector.IdleSeconds()
			line, err := json.Marshal(out)
			if err != nil {
				acc = map[string]int{}
				continue
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				return err // pipe closed — parent gone
			}
			if err := w.Flush(); err != nil {
				return err
			}
			acc = map[string]int{}
		}
	}
}

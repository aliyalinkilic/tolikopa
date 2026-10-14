package service

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/aliyalinkilic/tolikopa/agent/internal/collector"
)

// The --fg-helper runs in the interactive user's session AS THAT USER, so
// everything it writes to the pipe is untrusted input: a standard user can
// replace or impersonate the helper and emit whatever they like. The parent
// therefore treats the stream defensively — bounded line length, bounded
// entries per line, validated names, clamped seconds — so a hostile helper can
// at worst misreport its own user's app usage, never inflate a batch past the
// server's caps (which would get the whole batch, metrics and audit included,
// rejected).
const (
	// fgMaxLine bounds one protocol line. A genuine line is a few hundred bytes
	// (≤ ~8 apps per 30 s report); anything longer is discarded unread.
	fgMaxLine = 16 << 10
	// fgMaxEntriesPerLine bounds the app names taken from one line. The genuine
	// helper polls every 4 s and reports every 30 s, so it can name at most ~8.
	fgMaxEntriesPerLine = 32
	// fgMaxSecPerLine clamps one app's seconds in one line. A report covers 30 s;
	// the slack absorbs a delayed report without letting one line claim hours.
	fgMaxSecPerLine = 120
)

// focusSink receives validated per-app foreground seconds (the sender).
type focusSink interface {
	AddAppFocus(name string, seconds int)
}

// idleSink receives the "__idle" control value (the session tracker).
type idleSink interface{ ReportIdle(seconds int) }

// readHelperLines consumes helper protocol lines from r until EOF or a read
// error. Lines longer than fgMaxLine, and lines that are not a JSON object of
// name -> integer, are discarded whole.
func readHelperLines(r io.Reader, focus focusSink, idle idleSink) {
	br := bufio.NewReaderSize(r, fgMaxLine)
	discarding := false
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Oversize line: drop this chunk and everything up to the next newline.
			discarding = true
			continue
		}
		if err != nil {
			// EOF (or a broken pipe). A trailing line without '\n' is incomplete
			// by definition — the helper always terminates its lines — so drop it.
			return
		}
		if discarding {
			discarding = false // this read ended the oversize line
			continue
		}
		handleHelperLine(line, focus, idle)
	}
}

// handleHelperLine applies one complete protocol line. Keys prefixed with "__"
// are reserved control fields (never app names): "__idle" carries seconds since
// last input and feeds the session tracker; unknown control keys are dropped.
func handleHelperLine(line []byte, focus focusSink, idle idleSink) {
	var m map[string]int
	if err := json.Unmarshal(line, &m); err != nil {
		return // garbage: discard
	}
	taken := 0
	for name, sec := range m {
		if strings.HasPrefix(name, "__") {
			if name == "__idle" && sec >= 0 {
				idle.ReportIdle(sec)
			}
			continue
		}
		if taken >= fgMaxEntriesPerLine {
			continue
		}
		n, ok := collector.ValidAppName(name)
		if !ok || sec <= 0 {
			continue
		}
		if sec > fgMaxSecPerLine {
			sec = fgMaxSecPerLine
		}
		taken++
		focus.AddAppFocus(n, sec)
	}
}

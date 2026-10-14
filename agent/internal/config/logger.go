package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// maxLogBytes caps agent.log before it is rotated to agent.log.1. One previous
// generation is kept — enough to cover a restart loop, bounded on an endpoint we
// do not want to fill. (bootstrap.ps1 trims its own log the same way.)
const maxLogBytes = 5 << 20

// fanoutWriter writes to every sink and never lets one failing sink silence
// another.
//
// This exists because of a real outage of the log itself: the previous
// implementation used io.MultiWriter(os.Stderr, file), and io.MultiWriter stops
// at the FIRST writer that errors and never reaches the rest. A Windows service
// has no console, so os.Stderr is an invalid handle, every write failed there,
// and agent.log was never written — for weeks, on the whole fleet. The only lines
// it ever contained came from manual foreground runs, where stderr happened to be
// valid.
//
// A logger must also never be able to break the program, so write errors are
// deliberately swallowed: there is no more privileged place to report them to.
type fanoutWriter struct{ sinks []io.Writer }

func (f fanoutWriter) Write(p []byte) (int, error) {
	for _, w := range f.sinks {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

// rotate moves an oversized log aside, keeping exactly one previous generation.
func rotate(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= maxLogBytes {
		return
	}
	prev := path + ".1"
	_ = os.Remove(prev)
	_ = os.Rename(path, prev)
}

// NewLogger builds a slog.Logger writing to logs/agent.log and, when it is
// usable, stderr. The file handle is intentionally kept open for the process
// lifetime.
//
// The file is the sink that matters: as a service the agent has no console, and
// stderr is kept only so `tolikopa-agent.exe run` and the short-lived worker children stay
// debuggable from a terminal.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	sinks := []io.Writer{os.Stderr}
	if err := os.MkdirAll(LogDir(), 0o755); err == nil {
		path := filepath.Join(LogDir(), "agent.log")
		rotate(path)
		if f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640); ferr == nil {
			sinks = append(sinks, f)
		}
	}

	return slog.New(slog.NewTextHandler(fanoutWriter{sinks}, &slog.HandlerOptions{Level: lvl}))
}

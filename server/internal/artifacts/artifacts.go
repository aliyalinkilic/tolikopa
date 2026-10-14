// Package artifacts serves the build outputs the Docker image ships next to the
// server: the agent binaries, the generic setup tool and bootstrap.ps1, plus
// artifacts.json describing them.
//
// The directory comes from ARTIFACTS_DIR and is optional: in local development
// it does not exist, and every caller must cope with ErrUnavailable. File names
// are a closed set — nothing request-controlled is ever joined into a path.
package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The files the image provides.
const (
	AgentWindows = "tolikopa-agent.exe"
	AgentLinux   = "tolikopa-agent"
	SetupTool    = "tolikopa-setup.exe"
	Bootstrap    = "bootstrap.ps1"
	metaFile     = "artifacts.json"
)

// known is the closed set of readable names.
var known = map[string]bool{AgentWindows: true, AgentLinux: true, SetupTool: true, Bootstrap: true}

// rolloutFiles must all be present for the rollout tools to be offered.
var rolloutFiles = []string{AgentWindows, SetupTool, Bootstrap}

// maxFileSize bounds a single artifact read into memory (the agent is ~8 MB).
const maxFileSize = 128 << 20

// ErrUnavailable means the directory, its metadata, or a required file is
// missing or unreadable.
var ErrUnavailable = errors.New("artifacts unavailable")

// Meta is artifacts.json.
type Meta struct {
	ProductVersion string `json:"product_version"`
	AgentVersion   string `json:"agent_version"`
	BuiltAt        string `json:"built_at"` // RFC 3339
}

// Info is what the panel shows about the shipped build.
type Info struct {
	ProductVersion string `json:"product_version"`
	AgentVersion   string `json:"agent_version"`
	AgentSHA256    string `json:"agent_sha256"` // of tolikopa-agent.exe
	BuiltAt        string `json:"built_at"`
}

type sumEntry struct {
	mtime time.Time
	size  int64
	sum   string
}

// Store reads artifacts from one directory, caching file hashes.
type Store struct {
	dir string

	mu   sync.Mutex
	sums map[string]sumEntry
}

// New returns a store rooted at dir. An empty or missing dir is valid: every
// method then reports ErrUnavailable.
func New(dir string) *Store {
	return &Store{dir: dir, sums: make(map[string]sumEntry)}
}

func (s *Store) path(name string) (string, error) {
	if s.dir == "" || (!known[name] && name != metaFile) {
		return "", ErrUnavailable
	}
	return filepath.Join(s.dir, name), nil
}

// Sum returns the lowercase hex SHA-256 of a known artifact. The hash is cached
// and recomputed only when the file's mtime or size changes.
func (s *Store) Sum(name string) (string, error) {
	p, err := s.path(name)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	s.mu.Lock()
	e, ok := s.sums[name]
	s.mu.Unlock()
	if ok && e.size == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		return e.sum, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxFileSize+1)); err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	s.mu.Lock()
	s.sums[name] = sumEntry{mtime: fi.ModTime(), size: fi.Size(), sum: sum}
	s.mu.Unlock()
	return sum, nil
}

// Read returns a known artifact's bytes together with their SHA-256. The hash
// is computed from the bytes returned, so the two can never disagree.
func (s *Store) Read(name string) ([]byte, string, error) {
	p, err := s.path(name)
	if err != nil || !known[name] {
		return nil, "", ErrUnavailable
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil || len(data) == 0 || len(data) > maxFileSize {
		return nil, "", fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	return data, SumBytes(data), nil
}

// Meta reads and validates artifacts.json.
func (s *Store) Meta() (Meta, error) {
	var m Meta
	p, err := s.path(metaFile)
	if err != nil {
		return m, err
	}
	f, err := os.Open(p)
	if err != nil {
		return m, fmt.Errorf("%w: %s", ErrUnavailable, metaFile)
	}
	defer f.Close()
	if err := json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&m); err != nil {
		return m, fmt.Errorf("%w: %s is not valid JSON", ErrUnavailable, metaFile)
	}
	m.ProductVersion = strings.TrimSpace(m.ProductVersion)
	m.AgentVersion = strings.TrimSpace(m.AgentVersion)
	if m.AgentVersion == "" {
		return m, fmt.Errorf("%w: %s has no agent_version", ErrUnavailable, metaFile)
	}
	return m, nil
}

// Info describes the shipped build. It succeeds only when artifacts.json and
// every file the rollout tools need are present — i.e. when the panel can
// actually offer a working download.
func (s *Store) Info() (*Info, error) {
	m, err := s.Meta()
	if err != nil {
		return nil, err
	}
	var agentSum string
	for _, name := range rolloutFiles {
		sum, err := s.Sum(name)
		if err != nil {
			return nil, err
		}
		if name == AgentWindows {
			agentSum = sum
		}
	}
	return &Info{
		ProductVersion: m.ProductVersion, AgentVersion: m.AgentVersion,
		AgentSHA256: agentSum, BuiltAt: m.BuiltAt,
	}, nil
}

// SumBytes is the lowercase hex SHA-256 of data.
func SumBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

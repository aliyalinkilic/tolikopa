// Command synccatalog copies the agent's CANONICAL remediation catalog
// (agent/internal/appcatalog/catalog.json) byte-for-byte into the server module
// (server/internal/appcatalog/catalog.json).
//
// WHY A COPY AT ALL: agent/ and server/ are two independent Go modules that never
// import each other (CLAUDE.md), and go:embed refuses patterns containing ".."
// — a file outside the module directory simply cannot be embedded. A physical
// copy is therefore unavoidable. What this tool buys is that the copy is
// GENERATED and DIFF-CHECKED rather than hand-synced: drift becomes a failing CI
// step instead of a silent catalog-version mismatch that marks every deployed
// agent stale.
//
// Usage (from the repository root; Go is not installed locally):
//
//	docker run --rm -v "$PWD:/repo" -w /repo/server golang:1.26 \
//	  go run ./tools/synccatalog
//
// CI gate — run the same command, then assert nothing changed:
//
//	git diff --exit-code server/internal/appcatalog/catalog.json
//
// A non-zero exit there means somebody edited one copy without the other; re-run
// the tool and commit the result.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aliyalinkilic/tolikopa/server/internal/appcatalog"
)

func main() {
	log.SetFlags(0)

	root := repoRoot()
	src := flag.String("src", filepath.Join(root, "agent", "internal", "appcatalog", "catalog.json"),
		"canonical catalog (the agent's copy)")
	dst := flag.String("dst", filepath.Join(root, "server", "internal", "appcatalog", "catalog.json"),
		"generated catalog (the server's copy)")
	check := flag.Bool("check", false, "verify the copies are identical; do not write")
	flag.Parse()

	data, err := os.ReadFile(*src)
	if err != nil {
		log.Fatalf("synccatalog: read canonical catalog: %v\n"+
			"hint: run from the repo root with the whole tree mounted, or pass -src/-dst", err)
	}

	// Parse before writing: a malformed or duplicate-bearing catalog must fail the
	// sync rather than land in the server binary, where load() would panic at boot.
	items, version, err := appcatalog.Parse(data)
	if err != nil {
		log.Fatalf("synccatalog: refusing to copy an invalid catalog: %v", err)
	}

	if *check {
		cur, err := os.ReadFile(*dst)
		if err != nil {
			log.Fatalf("synccatalog: read generated catalog: %v", err)
		}
		if string(cur) != string(data) {
			log.Fatalf("synccatalog: %s is out of date — re-run without -check and commit", *dst)
		}
		fmt.Printf("synccatalog: up to date (%d items, %s)\n", len(items), version)
		return
	}

	if err := os.MkdirAll(filepath.Dir(*dst), 0o755); err != nil {
		log.Fatalf("synccatalog: %v", err)
	}
	// Byte-for-byte: no re-marshalling. Re-encoding would reorder keys and reflow
	// the Turkish notes, producing a diff on every run and defeating the CI gate.
	if err := os.WriteFile(*dst, data, 0o644); err != nil {
		log.Fatalf("synccatalog: write generated catalog: %v", err)
	}
	fmt.Printf("synccatalog: wrote %s (%d items, catalog version %s)\n", *dst, len(items), version)
}

// repoRoot derives the repository root from this source file's location
// (server/tools/synccatalog/main.go -> ../../..), so the tool works with no flags
// when the whole tree is mounted. Falls back to "." when the build stripped paths.
func repoRoot() string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", ".."))
}

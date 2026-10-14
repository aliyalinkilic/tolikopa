package actuator

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
)

// The removal worker protocol.
//
// One request line in on stdin, one response line out on stdout, then the child
// exits. Deliberately tiny: it carries a package identity and nothing else — no
// paths, no user context, no configuration, no token.
type workerRequest struct {
	Op       string   `json:"op"`                 // "resolve" | "remove" | "deprovision"
	Family   string   `json:"family"`             // PackageFamilyName ("remove"/"deprovision")
	FullName string   `json:"full_name"`          // PackageFullName ("remove" only)
	Families []string `json:"families,omitempty"` // PackageFamilyNames ("resolve")
}

type workerResponse struct {
	Outcome string `json:"outcome"`           // ok | absent | failed | protected | timeout
	HResult string `json:"hresult,omitempty"` // "0x…" code only, never a message
	// Packages answers "resolve": family -> the PackageFullNames Windows currently
	// reports for it, across all users. A family with nothing deployed is simply
	// absent from the map. Local detail — the parent feeds these names back to
	// "remove" and they never reach the wire.
	Packages map[string][]string `json:"packages,omitempty"`
}

// Worker operations.
const (
	opResolve     = "resolve"
	opRemove      = "remove"
	opDeprovision = "deprovision"
)

// Worker exit codes.
const (
	exitOK       = 0
	exitFailed   = 1
	exitTimeout  = 2
	exitRejected = 3 // the worker's own allowlist check refused the family
	exitAbsent   = 4 // nothing to do: the package was not deployed
)

// Worker outcome strings (a superset of the model's: "protected" maps to the
// model's "blocked" in the parent, "absent" to "not_present").
//
// "absent" is a SUCCESSFUL outcome, not a failure. A reconciler's goal is that
// the package is gone; Windows answering ERROR_NOT_FOUND means it already is.
// Reporting that as "failed" is what made every hygiene pass look broken while
// the endpoint was in exactly the desired state.
const (
	wOutcomeOK        = "ok"
	wOutcomeAbsent    = "absent"
	wOutcomeFailed    = "failed"
	wOutcomeProtected = "protected"
	wOutcomeTimeout   = "timeout"
)

// errNotFound is HRESULT_FROM_WIN32(ERROR_NOT_FOUND): the deployment API does not
// know this package identity. From a reconciler's point of view that is "already
// gone", never a failure.
const errNotFound = 0x80070490

// workerLineLimit bounds both directions of the protocol. The payload is three
// short strings; anything larger is a bug or an attempt to make the reader
// allocate.
const workerLineLimit = 64 << 10

// readWorkerRequest reads the single request line from stdin.
func readWorkerRequest() (workerRequest, error) {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 4096), workerLineLimit)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return workerRequest{}, err
		}
		return workerRequest{}, errors.New("no request line")
	}
	var req workerRequest
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		return workerRequest{}, err
	}
	return req, nil
}

// writeWorkerResponse writes the single response line to stdout. stdout carries
// the protocol and nothing else; the worker's logs go to stderr.
func writeWorkerResponse(resp workerResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		b = []byte(`{"outcome":"failed"}`)
	}
	_, _ = os.Stdout.Write(append(b, '\n'))
}

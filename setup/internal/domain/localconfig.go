package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aliyalinkilic/tolikopa/setup/internal/payload"
)

// Switches are the agent's local opt-ins for endpoint hygiene. They decide
// whether the agent may remove allowlisted software at all, so they are never
// taken from the server's bundle: the admin sets them here, on their own
// machine, and later runs keep whatever the share already says.
type Switches struct {
	AllowAppRemoval     bool
	AllowStartupControl bool
	EnforceActions      bool
}

// bundleKeys is the closed set of config.json keys accepted from the bundle. It is
// an allowlist on purpose: removing the switch names instead would miss spellings
// that a JSON decoder still folds onto them ("allow_\u017ftartup_control").
var bundleKeys = []string{"backend_url", "ingest_token", "tls_ca_pem", "intervals", "top_n", "spool_max_mb", "log_level"}

func sharedConfigPath(m *payload.Manifest) string {
	return filepath.Join(m.SharePath(), "config.json")
}

func readObject(data []byte) map[string]json.RawMessage {
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj
}

// CurrentSwitches reads the opt-ins from the config.json already on the share.
// A missing or unreadable file means everything is off.
func CurrentSwitches(m *payload.Manifest) Switches {
	var s Switches
	data, err := os.ReadFile(sharedConfigPath(m))
	if err != nil {
		return s
	}
	obj := readObject(data)
	flag := func(key string) bool {
		var v bool
		_ = json.Unmarshal(obj[key], &v)
		return v
	}
	s.AllowAppRemoval = flag("allow_app_removal")
	s.AllowStartupControl = flag("allow_startup_control")
	s.EnforceActions = flag("enforce_actions")
	return s
}

// MergeConfig builds the config.json to publish: connection settings from the
// bundle, the local keys from the existing file, and the switches as chosen.
// Any other key in the bundle is dropped.
func MergeConfig(bundle, existing []byte, s Switches) ([]byte, error) {
	in := readObject(bundle)
	if in == nil {
		return nil, fmt.Errorf("config.json in the bundle is not valid JSON")
	}
	out := map[string]json.RawMessage{}
	for _, k := range bundleKeys {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	if old := readObject(existing); old != nil {
		if v, ok := old["hygiene_hours"]; ok {
			out["hygiene_hours"] = v
		}
	}
	set := func(key string, v bool) {
		if v {
			out[key] = json.RawMessage("true")
		} else {
			out[key] = json.RawMessage("false")
		}
	}
	set("allow_app_removal", s.AllowAppRemoval)
	set("allow_startup_control", s.AllowStartupControl)
	set("enforce_actions", s.EnforceActions)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil { // map keys are written sorted: stable output
		return nil, err
	}
	return buf.Bytes(), nil
}

package ingest

import (
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"chrome.exe":                   "chrome.exe",
		`C:\Program Files\App\app.exe`: "app.exe",
		"/usr/lib/firefox/firefox":     "firefox",
		`C:\mixed/path\to/tool.exe`:    "tool.exe",
		`trailing\`:                    "",
		"":                             "",
	} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClipStringsStripsEventProcessPath(t *testing.T) {
	env := &model.Envelope{Events: []model.Event{{Kind: "app_crash", ProcessName: `C:\Users\x\AppData\evil.exe`}}}
	clipStrings(env)
	if got := env.Events[0].ProcessName; got != "evil.exe" {
		t.Fatalf("event process_name = %q, want evil.exe", got)
	}
}

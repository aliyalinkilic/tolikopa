package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
)

func TestNormalizeIgnoreApps(t *testing.T) {
	got, err := normalizeIgnoreApps([]string{" VendorSvc64.exe ", "vendorsvc64", "", "  ", "OemHelperService", "a.EXE"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vendorsvc64", "oemhelperservice", "a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, err := normalizeIgnoreApps(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil input: %v %v (want non-nil empty)", got, err)
	}

	for _, bad := range []string{`C:\Program Files\x.exe`, "bin/app", "a\tb", strings.Repeat("x", maxIgnoreAppLen+1)} {
		_, err := normalizeIgnoreApps([]string{bad})
		if status, b := apierr.Resolve(err); status != http.StatusBadRequest || b.Code != apierr.InvalidRequest {
			t.Errorf("%q: got %d %q, want 400 invalid_request", bad, status, b.Code)
		}
	}
	// Exactly the cap passes; one more is rejected. Duplicates do not count.
	many := make([]string, 0, maxIgnoreApps+1)
	for i := 0; i < maxIgnoreApps; i++ {
		many = append(many, "app"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	many = append(many, many[0])
	if got, err := normalizeIgnoreApps(many); err != nil || len(got) != maxIgnoreApps {
		t.Fatalf("cap: %d %v", len(got), err)
	}
	many = append(many, "onemore")
	_, err = normalizeIgnoreApps(many)
	if status, b := apierr.Resolve(err); status != http.StatusBadRequest || b.Code != "too_many_items" {
		t.Fatalf("over cap: got %d %q", status, b.Code)
	}
}

func TestDecodeIgnoreApps(t *testing.T) {
	got := decodeIgnoreApps([]byte(`[" VendorSvc64.exe ", 3, "", "vendorsvc64", "Foo"]`))
	if strings.Join(got, ",") != "vendorsvc64,foo" {
		t.Fatalf("got %v", got)
	}
	for _, raw := range []string{"", "null", `"x"`, `{}`} {
		if got := decodeIgnoreApps([]byte(raw)); got == nil || len(got) != 0 {
			t.Errorf("%q: %v", raw, got)
		}
	}
}

func TestPutAlertRulesRejectsBadIgnoreAppsBeforeDB(t *testing.T) {
	// A zero API (nil pool) proves validation runs before any database access.
	for _, body := range []string{
		`[{"rule":"cpu_high","enabled":true,"for_seconds":0,"ignore_apps":["x"]}]`,
		`[{"rule":"crash_storm","enabled":true,"for_seconds":0,"ignore_apps":["a/b"]}]`,
	} {
		err := (&API{}).PutAlertRules(jsonCtx(body))
		if status, _ := apierr.Resolve(err); status != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, status)
		}
	}
}

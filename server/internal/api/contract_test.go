package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

// Request-body contract tests: every write the panel makes must either be applied
// or be rejected loudly. These cover the validation that runs before a handler
// touches the database.

func jsonCtx(body string) echo.Context {
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	return echo.New().NewContext(req, httptest.NewRecorder())
}

func wantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	status, b := apierr.Resolve(err)
	if status != http.StatusBadRequest || b.Code != apierr.InvalidRequest {
		t.Fatalf("got %d %q (%v), want 400 invalid_request", status, b.Code, err)
	}
	if got, _ := b.Params["field"].(string); got != field {
		t.Fatalf("params.field = %q, want %q", got, field)
	}
}

func TestBindPolicyUpdate(t *testing.T) {
	// Exactly what RemovalSection sends.
	full := `{"enabled":true,"dry_run":true,"excluded_tags":["server"],"apps":["weather"],"startup":[]}`
	got, err := bindPolicyUpdate(jsonCtx(full))
	if err != nil {
		t.Fatalf("full body rejected: %v", err)
	}
	if !got.Enabled || !got.DryRun || len(got.Apps) != 1 || got.Startup == nil || got.Force {
		t.Fatalf("decoded wrong: %+v", got)
	}

	for _, tc := range []struct{ body, field string }{
		// A missing dry_run used to decode as false — dry-run silently switched off.
		{`{"enabled":true,"excluded_tags":[],"apps":[],"startup":[]}`, "dry_run"},
		// A missing apps used to wipe the whole app selection.
		{`{"enabled":false,"dry_run":true,"excluded_tags":[],"startup":[]}`, "apps"},
		{`{"enabled":false,"dry_run":true,"apps":[],"startup":[]}`, "excluded_tags"},
		{`{"policy":{"enabled":true}}`, "policy"},
		{`{"enabled":false,"dry_run":true,"excluded_tags":[],"apps":[],"startup":[],"revision":"7"}`, "revision"},
	} {
		_, err := bindPolicyUpdate(jsonCtx(tc.body))
		wantInvalid(t, err, tc.field)
	}
}

func TestValidateCategoryRule(t *testing.T) {
	r := model.CategoryRule{Category: " browser ", MatchValue: " Chrome ", Priority: 10}
	if err := validateCategoryRule(&r); err != nil {
		t.Fatal(err)
	}
	if r.Category != "browser" || r.MatchValue != "chrome" || r.MatchType != "exact" {
		t.Fatalf("not normalised: %+v", r)
	}
	for _, tc := range []struct {
		r     model.CategoryRule
		field string
	}{
		{model.CategoryRule{MatchValue: "x"}, "category"},
		{model.CategoryRule{Category: "browser"}, "match_value"},
		{model.CategoryRule{Category: "browser", MatchValue: "x", MatchType: "regex"}, "match_type"},
	} {
		wantInvalid(t, validateCategoryRule(&tc.r), tc.field)
	}
}

func TestMachineLabelsBody(t *testing.T) {
	var none machineLabelsBody
	if err := apierr.Bind(jsonCtx(`{}`), &none); err != nil {
		t.Fatal(err)
	}
	wantInvalid(t, none.validate(), "")

	// Tags only: labels must stay nil (= keep the stored value), not become {}.
	var tagsOnly machineLabelsBody
	if err := apierr.Bind(jsonCtx(`{"tags":[" lab ","lab",""]}`), &tagsOnly); err != nil {
		t.Fatal(err)
	}
	if err := tagsOnly.validate(); err != nil {
		t.Fatal(err)
	}
	if tagsOnly.Labels != nil || len(*tagsOnly.Tags) != 1 || (*tagsOnly.Tags)[0] != "lab" {
		t.Fatalf("got labels=%v tags=%v", tagsOnly.Labels, tagsOnly.Tags)
	}

	var both machineLabelsBody
	if err := apierr.Bind(jsonCtx(`{"labels":{"room":"101"},"tags":[]}`), &both); err != nil {
		t.Fatal(err)
	}
	if err := both.validate(); err != nil || both.Labels == nil || both.Tags == nil {
		t.Fatalf("labels+tags: err=%v %+v", err, both)
	}
}

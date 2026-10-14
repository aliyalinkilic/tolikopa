package apierr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func bindBody(t *testing.T, body string, ct string, dst any) error {
	t.Helper()
	e := echo.New()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPut, "/x", nil)
	} else {
		req = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	}
	if ct != "" {
		req.Header.Set(echo.HeaderContentType, ct)
	}
	return Bind(e.NewContext(req, httptest.NewRecorder()), dst)
}

func TestBind(t *testing.T) {
	type cfg struct {
		ServerURL *string `json:"server_url"`
		Flag      *bool   `json:"flag"`
	}
	cases := []struct {
		name      string
		body, ct  string
		status    int // 0 = success
		code      string
		wantField string
	}{
		{"flat fields", `{"server_url":"https://x"}`, "application/json", 0, "", ""},
		{"charset param", `{"flag":true}`, "application/json; charset=utf-8", 0, "", ""},
		{"no content type", `{"flag":true}`, "", 0, "", ""},
		// The bug this exists for: the wizard wrapped the fields in {config:{…}}.
		{"wrapped body", `{"config":{"server_url":"https://x"}}`, "application/json", 400, InvalidRequest, "config"},
		{"empty body", "", "application/json", 400, InvalidRequest, ""},
		{"wrong type", `{"flag":"yes"}`, "application/json", 400, InvalidRequest, "flag"},
		{"array for object", `[1]`, "application/json", 400, InvalidRequest, ""},
		{"trailing data", `{"flag":true}{}`, "application/json", 400, InvalidRequest, ""},
		{"malformed", `{"flag":`, "application/json", 400, InvalidRequest, ""},
		{"form body", `flag=1`, "application/x-www-form-urlencoded", 415, UnsupportedMediaType, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dst cfg
			err := bindBody(t, tc.body, tc.ct, &dst)
			if tc.status == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			status, b := Resolve(err)
			if status != tc.status || b.Code != tc.code {
				t.Fatalf("got %d %q, want %d %q (%v)", status, b.Code, tc.status, tc.code, err)
			}
			if got, _ := b.Params["field"].(string); got != tc.wantField {
				t.Fatalf("params.field = %q, want %q", got, tc.wantField)
			}
		})
	}
}

package apierr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// do runs one request through an Echo instance wired with Handler.
func do(t *testing.T, method string, h echo.HandlerFunc) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = Handler
	e.Add(http.MethodGet, "/x", h)
	req := httptest.NewRequest(method, "/x", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not JSON: %q", rec.Body.String())
		}
	}
	return rec, out
}

func TestHandlerShape(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"coded", New(http.StatusConflict, UsernameTaken, "username already exists"), 409, UsernameTaken},
		{"bare echo string", echo.NewHTTPError(http.StatusBadRequest, "türkçe bir şey"), 400, InvalidRequest},
		{"echo 413", echo.ErrStatusRequestEntityTooLarge, 413, PayloadTooLarge},
		{"plain error", errors.New("pq: relation admin_user does not exist"), 500, ServerError},
		{"wrapped 5xx text", echo.NewHTTPError(http.StatusBadGateway, "upstream secret detail"), 502, ServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := do(t, http.MethodGet, func(echo.Context) error { return tc.err })
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if body["code"] != tc.wantCode {
				t.Errorf("code = %v, want %s", body["code"], tc.wantCode)
			}
			msg, _ := body["message"].(string)
			if msg == "" {
				t.Error("message is empty")
			}
			// Nothing but the two (three with params) documented keys.
			for k := range body {
				if k != "code" && k != "message" && k != "params" {
					t.Errorf("unexpected key %q in error body", k)
				}
			}
		})
	}
}

// A free-form message must never leak: neither a non-English handler string nor
// internal error text reaches the client.
func TestHandlerDoesNotLeakFreeFormText(t *testing.T) {
	for _, err := range []error{
		echo.NewHTTPError(http.StatusBadRequest, "türkçe bir şey"),
		errors.New("pq: relation admin_user does not exist"),
		echo.NewHTTPError(http.StatusBadGateway, "upstream secret detail"),
	} {
		rec, _ := do(t, http.MethodGet, func(echo.Context) error { return err })
		for _, leak := range []string{"türkçe", "admin_user", "secret detail"} {
			if contains(rec.Body.String(), leak) {
				t.Errorf("response leaked %q: %s", leak, rec.Body.String())
			}
		}
	}
}

func TestHandlerRouterNotFound(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = Handler
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	var body Body
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 404 || body.Code != NotFound {
		t.Errorf("got %d %q, want 404 not_found", rec.Code, body.Code)
	}
}

func TestHandlerParams(t *testing.T) {
	_, body := do(t, http.MethodGet, func(echo.Context) error {
		return WithParams(http.StatusTooManyRequests, RateLimited, "too many attempts", map[string]any{"retry_after": 60})
	})
	p, _ := body["params"].(map[string]any)
	if p["retry_after"] != float64(60) {
		t.Errorf("params = %v", body["params"])
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

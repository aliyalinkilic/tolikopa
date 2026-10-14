// Package apierr defines the single error shape every API response uses:
//
//	{"code": "<snake_case>", "message": "<English fallback>", "params": {…}?}
//
// `code` is the machine-readable contract the panel localises from; `message` is
// an English fallback for logs, curl and unknown codes. `params` is optional and
// carries the values a localised string interpolates. No user-facing language
// lives on the server side of this boundary.
package apierr

import (
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Codes the panel depends on. Handler-specific codes are declared next to the
// handlers that return them; docs/API.md lists every code.
const (
	InvalidRequest     = "invalid_request"
	InvalidCredentials = "invalid_credentials"
	AccountDisabled    = "account_disabled"
	SessionRequired    = "session_required"
	SessionInvalid     = "session_invalid"
	CSRFFailed         = "csrf_failed"
	AdminRequired      = "admin_required"
	RateLimited        = "rate_limited"
	MFARequired        = "mfa_required"
	MFAInvalid         = "mfa_invalid"
	MFATokenInvalid    = "mfa_token_invalid"
	MFAAlreadyEnabled  = "mfa_already_enabled"
	MFANotEnabled      = "mfa_not_enabled"
	MFASetupRequired   = "mfa_setup_required"

	// "Require 2FA" policy.
	MFAEnrollmentRequired = "mfa_enrollment_required"
	MFARequiredByPolicy   = "mfa_required_by_policy"
	EnableOwn2FAFirst     = "enable_own_2fa_first"

	PasswordTooWeak   = "password_too_weak"
	PasswordTooLong   = "password_too_long"
	PasswordIncorrect = "password_incorrect"
	UsernameTaken     = "username_taken"
	UserNotFound      = "user_not_found"
	LastAdmin         = "last_admin"
	CannotModifySelf  = "cannot_modify_self"
	NotFound          = "not_found"
	ServerError       = "server_error"

	// First-run setup and rollout downloads.
	SetupAlreadyDone     = "setup_already_done"
	SetupCodeInvalid     = "setup_code_invalid"
	SetupIncomplete      = "setup_incomplete"
	ArtifactsUnavailable = "artifacts_unavailable"

	Forbidden            = "forbidden"
	MethodNotAllowed     = "method_not_allowed"
	PayloadTooLarge      = "payload_too_large"
	UnsupportedMediaType = "unsupported_media_type"
	ServerBusy           = "server_busy"
)

// Body is the JSON error document.
type Body struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

// New builds an error the central handler renders as Body with the given status.
func New(status int, code, message string) *echo.HTTPError {
	return echo.NewHTTPError(status, Body{Code: code, Message: message})
}

// WithParams is New plus interpolation values for the client-side message.
func WithParams(status int, code, message string, params map[string]any) *echo.HTTPError {
	return echo.NewHTTPError(status, Body{Code: code, Message: message, Params: params})
}

// BadRequest is a 400 invalid_request.
func BadRequest(message string) *echo.HTTPError {
	return New(http.StatusBadRequest, InvalidRequest, message)
}

// NotFoundf is a 404 not_found.
func NotFoundf(message string) *echo.HTTPError {
	return New(http.StatusNotFound, NotFound, message)
}

// Internal logs the real cause and returns a generic 500 — raw driver/SQL text
// (table names, constraints) must never reach the client.
func Internal(scope string, err error) *echo.HTTPError {
	log.Printf("%s: internal error: %v", scope, err)
	return New(http.StatusInternalServerError, ServerError, "internal server error")
}

// Handler is the Echo HTTPErrorHandler: it renders every error — ours, Echo's
// own (404/405/413 from the router and middleware) and unexpected ones — in the
// one Body shape.
func Handler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status, body := Resolve(err)
	if status >= 500 && body.Code == ServerError {
		// Already logged by Internal() when it came from there; an error of any
		// other origin is logged here so it is never silently swallowed.
		var he *echo.HTTPError
		if !errors.As(err, &he) {
			log.Printf("api: unhandled error on %s %s: %v", c.Request().Method, c.Path(), err)
		}
	}
	var werr error
	if c.Request().Method == http.MethodHead {
		werr = c.NoContent(status)
	} else {
		werr = c.JSON(status, body)
	}
	if werr != nil {
		log.Printf("api: writing error response: %v", werr)
	}
}

// Resolve maps any error to the status + Body the handler will send.
func Resolve(err error) (int, Body) {
	var he *echo.HTTPError
	if !errors.As(err, &he) {
		return http.StatusInternalServerError, Body{Code: ServerError, Message: "internal server error"}
	}
	status := he.Code
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	switch m := he.Message.(type) {
	case Body:
		return status, m
	case *Body:
		if m != nil {
			return status, *m
		}
	}
	// An Echo-originated error (router 404/405, BodyLimit 413, …) or a handler
	// that returned a bare status. Never forward a free-form 5xx message.
	return status, Body{Code: codeForStatus(status), Message: messageForStatus(status)}
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return InvalidRequest
	case http.StatusUnauthorized:
		return SessionRequired
	case http.StatusForbidden:
		return Forbidden
	case http.StatusNotFound:
		return NotFound
	case http.StatusMethodNotAllowed:
		return MethodNotAllowed
	case http.StatusRequestEntityTooLarge:
		return PayloadTooLarge
	case http.StatusUnsupportedMediaType:
		return UnsupportedMediaType
	case http.StatusTooManyRequests:
		return RateLimited
	}
	if status >= 500 {
		return ServerError
	}
	return InvalidRequest
}

func messageForStatus(status int) string {
	if status >= 500 {
		return "internal server error"
	}
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "request failed"
}

package apierr

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Bind decodes a write request's JSON body into dst, strictly:
//
//   - an empty body is a 400 invalid_request;
//   - a field dst does not declare is a 400 invalid_request with params.field
//     naming it — a body wrapped in the wrong envelope ({"config":{…}} instead of
//     the fields themselves) must fail loudly instead of "succeeding" while
//     changing nothing;
//   - a value of the wrong JSON type is a 400 invalid_request with params.field;
//   - trailing data after the document is a 400.
//
// Echo's c.Bind does none of this: it accepts an empty body as a no-op and
// silently drops unknown fields, which is how panel/server drift stayed invisible.
// Handlers that need "at least one field" or "every field" still check that
// themselves — Bind only guarantees the body is a well-formed document of the
// declared shape.
func Bind(c echo.Context, dst any) error {
	req := c.Request()
	if ct := req.Header.Get(echo.HeaderContentType); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != echo.MIMEApplicationJSON {
			return New(http.StatusUnsupportedMediaType, UnsupportedMediaType, "request body must be application/json")
		}
	}
	if req.Body == nil || req.Body == http.NoBody {
		return BadRequest("request body is required")
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	// Exactly one JSON document.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var he *echo.HTTPError
		if errors.As(err, &he) {
			return he
		}
		return BadRequest("request body must be a single JSON document")
	}
	return nil
}

func decodeError(err error) error {
	// BodyLimit reports an oversized body as a read error carrying its own 413.
	var he *echo.HTTPError
	if errors.As(err, &he) {
		return he
	}
	if errors.Is(err, io.EOF) {
		return BadRequest("request body is required")
	}
	// encoding/json has no typed error for an unknown field; its text is stable:
	//   json: unknown field "config"
	if rest, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		field := strings.Trim(rest, `"`)
		return WithParams(http.StatusBadRequest, InvalidRequest,
			"unknown field in request body: "+field, map[string]any{"field": field})
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		if te.Field != "" {
			return WithParams(http.StatusBadRequest, InvalidRequest,
				"wrong type for field "+te.Field, map[string]any{"field": te.Field})
		}
		return BadRequest("request body has the wrong JSON type")
	}
	return BadRequest("invalid JSON body")
}

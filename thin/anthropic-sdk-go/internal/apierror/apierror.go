// Package apierror carries the error returned for a non-2xx API response, or
// for an in-band `type: error` SSE event. It lives in an internal package so
// that both the root package and ssestream can build one.
package apierror

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/anthropics/anthropic-sdk-go/shared"
)

// Error is an error returned by the Anthropic API.
type Error struct {
	StatusCode  int
	Request     *http.Request
	Response    *http.Response
	RequestID   string
	WorkspaceID string

	raw       string
	errorType shared.ErrorType
}

// Type returns the `error.type` from the response body, e.g. "rate_limit_error".
func (r *Error) Type() shared.ErrorType { return r.errorType }

// RawJSON returns the unmodified JSON received from the API.
func (r Error) RawJSON() string { return r.raw }

func (r *Error) UnmarshalJSON(data []byte) error {
	r.raw = string(data)
	var envelope struct {
		Error struct {
			Type shared.ErrorType `json:"type"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(data, &envelope) == nil {
		r.errorType = envelope.Error.Type
		if r.RequestID == "" {
			r.RequestID = envelope.RequestID
		}
	}
	return nil
}

func (r *Error) Error() string {
	statusInfo := fmt.Sprintf("%s %q: %d %s", r.Request.Method, r.Request.URL, r.Response.StatusCode, http.StatusText(r.Response.StatusCode))
	if r.RequestID != "" {
		statusInfo += fmt.Sprintf(" (Request-ID: %s)", r.RequestID)
	}
	if r.WorkspaceID != "" {
		statusInfo += fmt.Sprintf(" (Workspace-ID: %s)", r.WorkspaceID)
	}
	return fmt.Sprintf("%s %s", statusInfo, r.raw)
}

func (r *Error) DumpRequest(body bool) []byte {
	if r.Request.GetBody != nil {
		r.Request.Body, _ = r.Request.GetBody()
	}
	out, _ := httputil.DumpRequestOut(r.Request, body)
	return out
}

func (r *Error) DumpResponse(body bool) []byte {
	out, _ := httputil.DumpResponse(r.Response, body)
	return out
}

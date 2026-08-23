// Package apierror carries the error returned for a non-2xx API response.
package apierror

import (
	"fmt"
	"net/http"
	"net/http/httputil"

	"github.com/openai/openai-go/v3/internal/apijson"
	"github.com/openai/openai-go/v3/packages/respjson"
)

// Error is returned when the API answers with a status code >= 400. It is
// decoded from the response body's "error" member, and keeps the request and
// response around so callers can inspect status codes and headers
// (Retry-After, ...).
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
	Type    string `json:"type"`
	JSON    struct {
		Code        respjson.Field
		Message     respjson.Field
		Param       respjson.Field
		Type        respjson.Field
		ExtraFields map[string]respjson.Field
		Raw         string
	} `json:"-"`
	StatusCode int
	Request    *http.Request
	Response   *http.Response
}

// RawJSON returns the unmodified JSON received from the API.
func (r Error) RawJSON() string { return r.JSON.Raw }

func (r *Error) UnmarshalJSON(data []byte) error { return apijson.UnmarshalRoot(data, r) }

func (r *Error) Error() string {
	return fmt.Sprintf("%s %q: %d %s %s", r.Request.Method, r.Request.URL,
		r.Response.StatusCode, http.StatusText(r.Response.StatusCode), r.JSON.Raw)
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

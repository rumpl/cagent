package cloudbridge

import (
	"encoding/json"
	"fmt"
)

// APError represents a structured error returned by an Agentic Platform
// Connect-RPC call. The body is JSON of the form
// {"code":"not_found","message":"local session not found"}.
type APError struct {
	Method  string
	Status  int
	Code    string
	Message string
	Raw     []byte
}

func (e *APError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ap %s: HTTP %d %s: %s", e.Method, e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("ap %s: HTTP %d: %s", e.Method, e.Status, string(e.Raw))
}

// IsCode reports whether err is an *APError with the given Connect code.
func IsCode(err error, code string) bool {
	var ap *APError
	for err != nil {
		if e, ok := err.(*APError); ok {
			ap = e
			break
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return ap != nil && ap.Code == code
}

func newAPError(method string, status int, body []byte) error {
	e := &APError{Method: method, Status: status, Raw: body}
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Code = parsed.Code
		e.Message = parsed.Message
	}
	return e
}

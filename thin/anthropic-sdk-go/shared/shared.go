// Package shared contains the error shapes reused across the Messages and
// Beta Messages APIs.
package shared

import "encoding/json"

// ErrorType is the `error.type` discriminator of an API error body.
type ErrorType string

const (
	ErrorTypeInvalidRequestError ErrorType = "invalid_request_error"
	ErrorTypeAuthenticationError ErrorType = "authentication_error"
	ErrorTypeBillingError        ErrorType = "billing_error"
	ErrorTypePermissionError     ErrorType = "permission_error"
	ErrorTypeNotFoundError       ErrorType = "not_found_error"
	ErrorTypeRateLimitError      ErrorType = "rate_limit_error"
	ErrorTypeTimeoutError        ErrorType = "timeout_error"
	ErrorTypeAPIError            ErrorType = "api_error"
	ErrorTypeOverloadedError     ErrorType = "overloaded_error"
)

// ErrorObjectUnion is the flattened `error` object of an error response.
type ErrorObjectUnion struct {
	Message string `json:"message"`
	Type    string `json:"type"`

	raw string
}

func (r ErrorObjectUnion) RawJSON() string { return r.raw }

func (r *ErrorObjectUnion) UnmarshalJSON(data []byte) error {
	type shadow ErrorObjectUnion
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*r = ErrorObjectUnion(s)
	r.raw = string(data)
	return nil
}

// ErrorResponse is the top-level envelope of an error body.
type ErrorResponse struct {
	Error ErrorObjectUnion `json:"error"`
	Type  string           `json:"type"`
}

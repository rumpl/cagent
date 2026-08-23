// Package anthropic is a hand-written client for the subset of the Anthropic
// API that docker-agent uses: the Messages API, the Beta Messages API, and
// token counting.
package anthropic

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go/internal/apierror"
	"github.com/anthropics/anthropic-sdk-go/internal/requestconfig"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared"
)

// Error is an error returned by the Anthropic API.
type Error = apierror.Error

// ErrorObjectUnion, ErrorResponse and ErrorType alias the shared error shapes.
type (
	ErrorObjectUnion = shared.ErrorObjectUnion
	ErrorResponse    = shared.ErrorResponse
	ErrorType        = shared.ErrorType
)

const (
	ErrorTypeInvalidRequestError = shared.ErrorTypeInvalidRequestError
	ErrorTypeAuthenticationError = shared.ErrorTypeAuthenticationError
	ErrorTypeBillingError        = shared.ErrorTypeBillingError
	ErrorTypePermissionError     = shared.ErrorTypePermissionError
	ErrorTypeNotFoundError       = shared.ErrorTypeNotFoundError
	ErrorTypeRateLimitError      = shared.ErrorTypeRateLimitError
	ErrorTypeTimeoutError        = shared.ErrorTypeTimeoutError
	ErrorTypeAPIError            = shared.ErrorTypeAPIError
	ErrorTypeOverloadedError     = shared.ErrorTypeOverloadedError
)

// String, Int, Bool, Float and Time build the optional request parameters.
func String(s string) param.Opt[string]     { return param.NewOpt(s) }
func Int(i int64) param.Opt[int64]          { return param.NewOpt(i) }
func Bool(b bool) param.Opt[bool]           { return param.NewOpt(b) }
func Float(f float64) param.Opt[float64]    { return param.NewOpt(f) }
func Time(t time.Time) param.Opt[time.Time] { return param.NewOpt(t) }

func Opt[T comparable](v T) param.Opt[T] { return param.NewOpt(v) }
func Ptr[T any](v T) *T                  { return &v }

// Client is an Anthropic API client. The zero value is unusable; build one
// with [NewClient].
type Client struct {
	Options []option.RequestOption

	Messages MessageService
	Beta     BetaService
}

// NewClient returns a client configured with opts, on top of the defaults
// read from the environment (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN,
// ANTHROPIC_BASE_URL).
func NewClient(opts ...option.RequestOption) (r Client) {
	defaults := []option.RequestOption{option.WithEnvironmentProduction()}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		defaults = append(defaults, option.WithBaseURL(base))
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		defaults = append(defaults, option.WithAPIKey(key))
	}
	if token := os.Getenv("ANTHROPIC_AUTH_TOKEN"); token != "" {
		defaults = append(defaults, option.WithAuthToken(token))
	}

	r.Options = append(defaults, opts...)
	r.Messages = MessageService{Options: r.Options}
	r.Beta = BetaService{Options: r.Options, Messages: BetaMessageService{Options: r.Options}}
	return r
}

// MessageService talks to the Messages API.
type MessageService struct {
	Options []option.RequestOption
}

// BetaService groups the Beta APIs.
type BetaService struct {
	Options  []option.RequestOption
	Messages BetaMessageService
}

// BetaMessageService talks to the Beta Messages API.
type BetaMessageService struct {
	Options []option.RequestOption
}

func execute(ctx context.Context, path string, body any, dst any, base []option.RequestOption, opts []option.RequestOption) error {
	all := make([]option.RequestOption, 0, len(base)+len(opts))
	all = append(all, base...)
	all = append(all, opts...)
	return requestconfig.ExecuteNewRequest(ctx, http.MethodPost, path, body, dst, all...)
}

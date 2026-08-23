// Package requestconfig owns the HTTP plumbing shared by every API method:
// option application, URL resolution, middleware, retries and response
// decoding.
package requestconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/internal/apierror"
)

// HTTPDoer is satisfied by *http.Client and by custom transports.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Middleware intercepts a request on its way to the transport. It mirrors
// option.Middleware, redeclared here to avoid an import cycle.
type (
	Middleware     = func(*http.Request, MiddlewareNext) (*http.Response, error)
	MiddlewareNext = func(*http.Request) (*http.Response, error)
)

// RequestOption mutates the configuration of a single request.
type RequestOption interface {
	Apply(cfg *RequestConfig) error
}

// RequestOptionFunc adapts a function to [RequestOption].
type RequestOptionFunc func(*RequestConfig) error

func (f RequestOptionFunc) Apply(cfg *RequestConfig) error { return f(cfg) }

// RequestConfig is all the state of one request.
type RequestConfig struct {
	MaxRetries     int
	Request        *http.Request
	BaseURL        *url.URL
	DefaultBaseURL *url.URL
	HTTPClient     HTTPDoer
	Middlewares    []Middleware
	APIKey         string
	// ResponseBodyInto receives the decoded body; a **http.Response instead
	// hands the still-open response to the caller (streaming).
	ResponseBodyInto any
	Body             []byte

	authHeaderOverride bool
}

func (cfg *RequestConfig) Apply(opts ...RequestOption) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt.Apply(cfg); err != nil {
			return err
		}
	}
	return nil
}

func (cfg *RequestConfig) SetHeader(key, value string) {
	cfg.Request.Header.Set(key, value)
	if strings.EqualFold(key, "Authorization") {
		cfg.authHeaderOverride = true
	}
}

func (cfg *RequestConfig) AddHeader(key, value string) { cfg.Request.Header.Add(key, value) }

func (cfg *RequestConfig) SetAPIKey(value string) { cfg.APIKey = value }

// NewRequestConfig prepares a request for path (relative to the base URL)
// carrying an already-encoded JSON body.
func NewRequestConfig(ctx context.Context, method, path string, body []byte, dst any, opts ...RequestOption) (*RequestConfig, error) {
	req, err := http.NewRequestWithContext(ctx, method, path, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range platformHeaders() {
		req.Header.Set(name, value)
	}

	cfg := RequestConfig{
		MaxRetries:       2,
		Request:          req,
		HTTPClient:       http.DefaultClient,
		Body:             body,
		ResponseBodyInto: dst,
	}
	if err := cfg.Apply(opts...); err != nil {
		return nil, err
	}
	if cfg.APIKey != "" && !cfg.authHeaderOverride {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	return &cfg, nil
}

// ExecuteNewRequest prepares and runs a request in one step.
func ExecuteNewRequest(ctx context.Context, method, path string, body []byte, dst any, opts ...RequestOption) error {
	cfg, err := NewRequestConfig(ctx, method, path, body, dst, opts...)
	if err != nil {
		return err
	}
	return cfg.Execute()
}

func (cfg *RequestConfig) Execute() error {
	if cfg.BaseURL == nil {
		if cfg.DefaultBaseURL == nil {
			return errors.New("requestconfig: base url is not set")
		}
		cfg.BaseURL = cfg.DefaultBaseURL
	}

	// The path is relative: a base URL path prefix (/v1/, an Azure
	// deployment, ...) must be preserved, and its query kept.
	resolved, err := cfg.BaseURL.Parse(strings.TrimLeft(cfg.Request.URL.String(), "/"))
	if err != nil {
		return err
	}
	if base := cfg.BaseURL.RawQuery; base != "" {
		if resolved.RawQuery == "" {
			resolved.RawQuery = base
		} else {
			resolved.RawQuery = base + "&" + resolved.RawQuery
		}
	}
	cfg.Request.URL = resolved

	if cfg.Body != nil {
		body := cfg.Body
		cfg.Request.ContentLength = int64(len(body))
		cfg.Request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		cfg.Request.Body, _ = cfg.Request.GetBody()
	}

	handler := cfg.HTTPClient.Do
	for _, middleware := range slices.Backward(cfg.Middlewares) {
		next := handler
		handler = func(req *http.Request) (*http.Response, error) { return middleware(req, next) }
	}

	var res *http.Response
	for retryCount := 0; ; retryCount++ {
		ctx := cfg.Request.Context()
		req := cfg.Request.Clone(ctx)
		if cfg.Request.GetBody != nil {
			req.Body, _ = cfg.Request.GetBody()
		}

		res, err = handler(req)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if retryCount >= cfg.MaxRetries || !shouldRetry(res, err) {
			break
		}
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay(res, retryCount)):
		}
	}

	if into, ok := cfg.ResponseBodyInto.(**http.Response); ok {
		*into = res
	}
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		return apiError(cfg.Request, res)
	}

	if _, streaming := cfg.ResponseBodyInto.(**http.Response); streaming || cfg.ResponseBodyInto == nil {
		return nil
	}

	contents, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}
	if dst, ok := cfg.ResponseBodyInto.(*[]byte); ok {
		*dst = contents
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("content-type"))
	if !strings.Contains(mediaType, "application/json") && !strings.HasSuffix(mediaType, "+json") {
		if dst, ok := cfg.ResponseBodyInto.(*string); ok {
			*dst = string(contents)
			return nil
		}
		return fmt.Errorf("expected destination type of 'string' or '[]byte' for responses with content-type %q that is not 'application/json'", res.Header.Get("content-type"))
	}
	if err := json.Unmarshal(contents, cfg.ResponseBodyInto); err != nil {
		return fmt.Errorf("error parsing response json: %w", err)
	}
	return nil
}

// apiError builds the *apierror.Error for a failed response, keeping the body
// readable for callers that dump it.
func apiError(req *http.Request, res *http.Response) error {
	contents, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	res.Body = io.NopCloser(bytes.NewBuffer(contents))

	aerr := apierror.Error{Request: req, Response: res, StatusCode: res.StatusCode}
	if err := aerr.UnmarshalJSON(errorMember(contents)); err != nil {
		return err
	}
	return &aerr
}

// errorMember extracts the "error" member of the body, which is where the API
// puts the failure details.
func errorMember(body []byte) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil
	}
	return obj["error"]
}

func shouldRetry(res *http.Response, err error) bool {
	if err != nil {
		// No response at all: a connection-level failure, worth retrying.
		return res == nil
	}
	switch res.Header.Get("x-should-retry") {
	case "true":
		return true
	case "false":
		return false
	}
	return res.StatusCode == http.StatusRequestTimeout ||
		res.StatusCode == http.StatusConflict ||
		res.StatusCode == http.StatusTooManyRequests ||
		res.StatusCode >= http.StatusInternalServerError
}

func retryDelay(res *http.Response, retryCount int) time.Duration {
	if after, ok := parseRetryAfterHeader(res); ok {
		return max(0, after)
	}
	delay := min(time.Duration(0.5*float64(time.Second)*math.Pow(2, float64(retryCount))), 8*time.Second)
	return delay - time.Duration(rand.Int63n(int64(delay/4)))
}

func parseRetryAfterHeader(res *http.Response) (time.Duration, bool) {
	if res == nil {
		return 0, false
	}
	if v := res.Header.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}
	v := res.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(v, 64); err == nil {
		return time.Duration(seconds * float64(time.Second)), true
	}
	if t, err := time.Parse(time.RFC1123, v); err == nil {
		return time.Until(t), true
	}
	return 0, false
}

// PackageVersion is the version of the API surface this client implements. It
// is reported in the User-Agent so servers see the same client identity as the
// upstream SDK.
const PackageVersion = "3.52.0"

// platformHeaders are the client-identity headers sent with every request.
func platformHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                  "OpenAI/Go " + PackageVersion,
		"X-Stainless-Lang":            "go",
		"X-Stainless-Package-Version": PackageVersion,
		"X-Stainless-OS":              normalizedOS(runtime.GOOS),
		"X-Stainless-Arch":            normalizedArch(runtime.GOARCH),
		"X-Stainless-Runtime":         "go",
		"X-Stainless-Runtime-Version": runtime.Version(),
	}
}

func normalizedOS(goos string) string {
	switch goos {
	case "ios":
		return "iOS"
	case "android":
		return "Android"
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "linux":
		return "Linux"
	default:
		return "Other:" + goos
	}
}

func normalizedArch(goarch string) string {
	switch goarch {
	case "386":
		return "x32"
	case "amd64":
		return "x64"
	case "arm", "arm64":
		return goarch
	default:
		return "other:" + goarch
	}
}

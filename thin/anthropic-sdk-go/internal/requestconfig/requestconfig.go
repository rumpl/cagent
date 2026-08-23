// Package requestconfig assembles, sends and retries a single API request.
// It is internal so that the option package can mutate a request without
// exposing the wire machinery.
package requestconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/internal/apierror"
)

// RequestOption mutates the configuration of a request before it is sent.
type RequestOption interface {
	Apply(*RequestConfig) error
}

// RequestOptionFunc adapts a function to RequestOption.
type RequestOptionFunc func(*RequestConfig) error

func (s RequestOptionFunc) Apply(r *RequestConfig) error { return s(r) }

// HTTPDoer is satisfied by *http.Client and by custom implementations.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Middleware intercepts a request on its way to the wire.
type Middleware = func(*http.Request, MiddlewareNext) (*http.Response, error)

// MiddlewareNext passes a request to the next stage of the chain.
type MiddlewareNext = func(*http.Request) (*http.Response, error)

// RequestConfig holds all the state of one in-flight request.
type RequestConfig struct {
	MaxRetries     int
	RequestTimeout time.Duration
	Context        context.Context
	Request        *http.Request
	BaseURL        *url.URL
	DefaultBaseURL *url.URL
	CustomHTTPDoer HTTPDoer
	HTTPClient     *http.Client
	Middlewares    []Middleware
	APIKey         string
	AuthToken      string
	// ResponseBodyInto receives the decoded body, or the raw *http.Response
	// when it is a **http.Response (the streaming case).
	ResponseBodyInto any
	Body             []byte
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

// WithDefaultBaseURL sets the base URL used when the caller sets none.
func WithDefaultBaseURL(baseURL string) RequestOption {
	u, err := url.Parse(baseURL)
	return RequestOptionFunc(func(r *RequestConfig) error {
		if err != nil {
			return fmt.Errorf("requestconfig: invalid default base url: %w", err)
		}
		r.DefaultBaseURL = u
		return nil
	})
}

// NewRequestConfig serializes body as JSON and applies opts to the request.
func NewRequestConfig(ctx context.Context, method, path string, body any, dst any, opts ...RequestOption) (*RequestConfig, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Anthropic/Go")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("X-Stainless-Lang", "go")

	cfg := RequestConfig{
		MaxRetries:       2,
		Context:          ctx,
		Request:          req,
		HTTPClient:       http.DefaultClient,
		Body:             payload,
		ResponseBodyInto: dst,
	}
	if err := cfg.Apply(opts...); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ExecuteNewRequest builds and runs a request in one step.
func ExecuteNewRequest(ctx context.Context, method, path string, body any, dst any, opts ...RequestOption) error {
	cfg, err := NewRequestConfig(ctx, method, path, body, dst, opts...)
	if err != nil {
		return err
	}
	return cfg.Execute()
}

func shouldRetry(req *http.Request, res *http.Response) bool {
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	// No response at all means a connection error, which is worth retrying.
	if res == nil {
		return true
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

func parseRetryAfterHeader(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	for _, h := range []struct {
		header string
		units  time.Duration
		date   bool
	}{
		{header: "Retry-After-Ms", units: time.Millisecond},
		{header: "Retry-After", units: time.Second, date: true},
	} {
		v := resp.Header.Get(h.header)
		if v == "" {
			continue
		}
		if retryAfter, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(retryAfter * float64(h.units)), true
		}
		if h.date {
			if t, err := time.Parse(time.RFC1123, v); err == nil {
				return time.Until(t), true
			}
		}
	}
	return 0, false
}

func retryDelay(res *http.Response, retryCount int) time.Duration {
	if retryAfterDelay, ok := parseRetryAfterHeader(res); ok {
		return max(0, retryAfterDelay)
	}
	delay := time.Duration(0.5 * float64(time.Second) * math.Pow(2, float64(retryCount)))
	delay = min(delay, 8*time.Second)
	return delay - time.Duration(rand.Int63n(int64(delay/4)))
}

func applyMiddleware(middleware Middleware, next MiddlewareNext) MiddlewareNext {
	return func(req *http.Request) (*http.Response, error) {
		return middleware(req, next)
	}
}

// Execute sends the request, retrying per the SDK's retry policy, and decodes
// the response into ResponseBodyInto.
func (cfg *RequestConfig) Execute() (err error) {
	if cfg.BaseURL == nil {
		if cfg.DefaultBaseURL == nil {
			return fmt.Errorf("requestconfig: base url is not set")
		}
		cfg.BaseURL = cfg.DefaultBaseURL
	}
	cfg.Request.URL, err = cfg.BaseURL.Parse(strings.TrimLeft(cfg.Request.URL.String(), "/"))
	if err != nil {
		return err
	}

	if cfg.Body != nil {
		body := cfg.Body
		cfg.Request.ContentLength = int64(len(body))
		cfg.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		cfg.Request.Body, _ = cfg.Request.GetBody()
	}

	handler := cfg.HTTPClient.Do
	if cfg.CustomHTTPDoer != nil {
		handler = cfg.CustomHTTPDoer.Do
	}
	for i := len(cfg.Middlewares) - 1; i >= 0; i-- {
		handler = applyMiddleware(cfg.Middlewares[i], handler)
	}

	var res *http.Response
	for retryCount := 0; retryCount <= cfg.MaxRetries; retryCount++ {
		ctx := cfg.Request.Context()
		if cfg.RequestTimeout != 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, cfg.RequestTimeout)
			defer cancel()
		}

		req := cfg.Request.Clone(ctx)
		if cfg.Request.GetBody != nil {
			req.Body, err = cfg.Request.GetBody()
			if err != nil {
				return err
			}
		}

		res, err = handler(req)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !shouldRetry(cfg.Request, res) || retryCount >= cfg.MaxRetries {
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
		contents, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return readErr
		}
		// Re-populate the body so debugging utilities can dump the response.
		res.Body = io.NopCloser(bytes.NewBuffer(contents))

		aerr := apierror.Error{
			Request:     cfg.Request,
			Response:    res,
			StatusCode:  res.StatusCode,
			RequestID:   res.Header.Get("request-id"),
			WorkspaceID: res.Header.Get("anthropic-workspace-id"),
		}
		if err := aerr.UnmarshalJSON(contents); err != nil {
			return err
		}
		return &aerr
	}

	if _, streaming := cfg.ResponseBodyInto.(**http.Response); streaming || cfg.ResponseBodyInto == nil {
		return nil
	}

	contents, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	res.Body = io.NopCloser(bytes.NewBuffer(contents))
	if len(contents) == 0 {
		return nil
	}
	return json.Unmarshal(contents, cfg.ResponseBodyInto)
}

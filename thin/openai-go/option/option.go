// Package option configures requests: credentials, endpoint, transport,
// headers and middleware.
package option

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/openai/openai-go/v3/internal/requestconfig"
)

// RequestOption mutates one request's configuration. Options passed to the
// client apply to every request it makes.
type RequestOption = requestconfig.RequestOption

// MiddlewareNext calls the rest of the chain.
type MiddlewareNext = func(*http.Request) (*http.Response, error)

// Middleware intercepts every HTTP request made by the client.
type Middleware = func(*http.Request, MiddlewareNext) (*http.Response, error)

// HTTPClient is primarily an *http.Client, but any Do implementation works.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// WithBaseURL points the client at another endpoint. A path is given a
// trailing slash so it is kept when the method path is resolved against it.
func WithBaseURL(base string) RequestOption {
	u, err := url.Parse(base)
	if err == nil && u.Path != "" && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if err != nil {
			return fmt.Errorf("requestoption: WithBaseURL failed to parse url: %w", err)
		}
		r.BaseURL = u
		return nil
	})
}

// WithHTTPClient replaces the client used to perform requests.
func WithHTTPClient(client HTTPClient) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if client == nil {
			return errors.New("requestoption: custom http client cannot be nil")
		}
		r.HTTPClient = client
		return nil
	})
}

// WithAPIKey sets the bearer credential.
func WithAPIKey(value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.SetAPIKey(value)
		return nil
	})
}

// WithHeader sets a header, replacing any existing value.
func WithHeader(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.SetHeader(key, value)
		return nil
	})
}

// WithHeaderAdd appends a header value.
func WithHeaderAdd(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.AddHeader(key, value)
		return nil
	})
}

// WithHeaderDel removes a header.
func WithHeaderDel(key string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Del(key)
		return nil
	})
}

// WithQueryAdd appends a query parameter to the request URL.
func WithQueryAdd(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		query := r.Request.URL.Query()
		query.Add(key, value)
		r.Request.URL.RawQuery = query.Encode()
		return nil
	})
}

// WithMiddleware installs middleware, executed in the order given.
func WithMiddleware(middlewares ...Middleware) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Middlewares = append(r.Middlewares, middlewares...)
		return nil
	})
}

// WithMaxRetries caps how many times a failed request is replayed.
func WithMaxRetries(retries int) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if retries < 0 {
			return errors.New("requestoption: cannot have fewer than 0 retries")
		}
		r.MaxRetries = retries
		return nil
	})
}

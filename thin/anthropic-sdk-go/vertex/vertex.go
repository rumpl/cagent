// Package vertex targets Claude models hosted on Google Cloud Vertex AI,
// which serve the Anthropic API under `:rawPredict` / `:streamRawPredict`
// endpoints and authenticate with OAuth2 instead of an API key.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2/google"

	"github.com/anthropics/anthropic-sdk-go/internal/requestconfig"
	sdkoption "github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultVersion is the `anthropic_version` body field Vertex AI expects.
const DefaultVersion = "vertex-2023-10-16"

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// WithGoogleAuth configures a client for Vertex AI using Application Default
// Credentials. Prefer [WithCredentials] when the credentials are already
// resolved: this function panics when they cannot be found.
func WithGoogleAuth(ctx context.Context, region, projectID string, scopes ...string) sdkoption.RequestOption {
	if region == "" {
		panic("region must be provided")
	}
	if len(scopes) == 0 {
		scopes = []string{cloudPlatformScope}
	}
	creds, err := google.FindDefaultCredentials(ctx, scopes...)
	if err != nil {
		panic(fmt.Errorf("failed to find default credentials: %v", err))
	}
	return WithCredentials(ctx, region, projectID, creds)
}

// WithCredentials points the client at the Vertex AI endpoint for region and
// projectID, authenticates it with creds, and installs the middleware that
// rewrites Anthropic-shaped requests into Vertex ones.
//
// Middleware runs in registration order, so register any
// [sdkoption.WithMiddleware] before this option if it should observe
// Anthropic-shaped requests.
func WithCredentials(_ context.Context, region, projectID string, creds *google.Credentials) sdkoption.RequestOption {
	middleware := vertexMiddleware(region, projectID)

	var baseURL string
	switch region {
	case "global":
		baseURL = "https://aiplatform.googleapis.com/"
	case "us":
		baseURL = "https://aiplatform.us.rep.googleapis.com/"
	case "eu":
		baseURL = "https://aiplatform.eu.rep.googleapis.com/"
	default:
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/", region)
	}

	return requestconfig.RequestOptionFunc(func(rc *requestconfig.RequestConfig) error {
		return rc.Apply(
			sdkoption.WithBaseURL(baseURL),
			sdkoption.WithMiddleware(middleware),
			sdkoption.WithMiddleware(authMiddleware(creds)),
		)
	})
}

// authMiddleware attaches an OAuth2 bearer token from creds, and drops the
// API key header a first-party client would otherwise send.
func authMiddleware(creds *google.Credentials) sdkoption.Middleware {
	return func(r *http.Request, next sdkoption.MiddlewareNext) (*http.Response, error) {
		if creds == nil || creds.TokenSource == nil {
			return nil, fmt.Errorf("vertex: no credentials")
		}
		token, err := creds.TokenSource.Token()
		if err != nil {
			return nil, fmt.Errorf("vertex: failed to obtain OAuth2 token: %w", err)
		}
		r.Header.Del("X-Api-Key")
		token.SetAuthHeader(r)
		return next(r)
	}
}

func vertexMiddleware(region, projectID string) sdkoption.Middleware {
	return func(r *http.Request, next sdkoption.MiddlewareNext) (*http.Response, error) {
		if r.Body == nil {
			return next(r)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		_ = r.Body.Close()

		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("vertex: request body is not a JSON object: %w", err)
		}
		if _, ok := payload["anthropic_version"]; !ok {
			payload["anthropic_version"], _ = json.Marshal(DefaultVersion)
		}

		switch {
		case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
			if projectID == "" {
				return nil, fmt.Errorf("no projectId was given and it could not be resolved from credentials")
			}
			var model string
			_ = json.Unmarshal(payload["model"], &model)
			var stream bool
			_ = json.Unmarshal(payload["stream"], &stream)
			delete(payload, "model")

			specifier := "rawPredict"
			if stream {
				specifier = "streamRawPredict"
			}
			r.URL.Path = fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:%s", projectID, region, model, specifier)
		case r.URL.Path == "/v1/messages/count_tokens" && r.Method == http.MethodPost:
			if projectID == "" {
				return nil, fmt.Errorf("no projectId was given and it could not be resolved from credentials")
			}
			r.URL.Path = fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/anthropic/models/count-tokens:rawPredict", projectID, region)
		}

		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader := bytes.NewReader(body)
		r.Body = io.NopCloser(reader)
		r.GetBody = func() (io.ReadCloser, error) {
			_, err := reader.Seek(0, 0)
			return io.NopCloser(reader), err
		}
		r.ContentLength = int64(len(body))

		return next(r)
	}
}

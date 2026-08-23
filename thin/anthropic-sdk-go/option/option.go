// Package option configures the requests made by the Anthropic client.
package option

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/internal/requestconfig"
)

// RequestOption is an option for the requests made by the Anthropic client.
type RequestOption = requestconfig.RequestOption

// HTTPClient describes an *http.Client, or any custom implementation.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Middleware intercepts HTTP requests, calling next to continue the chain.
type Middleware = requestconfig.Middleware

// MiddlewareNext passes a request to the next stage of the middleware chain.
type MiddlewareNext = requestconfig.MiddlewareNext

// WithBaseURL sets the base URL of the client. For security reasons, ensure
// the base URL is trusted.
func WithBaseURL(base string) RequestOption {
	u, err := url.Parse(base)
	if err == nil && u.Path != "" && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if err != nil {
			return fmt.Errorf("requestoption: WithBaseURL failed to parse url %s", err)
		}
		r.BaseURL = u
		return nil
	})
}

// WithHTTPClient changes the underlying HTTP client used for requests.
func WithHTTPClient(client HTTPClient) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if client == nil {
			return fmt.Errorf("requestoption: custom http client cannot be nil")
		}
		if c, ok := client.(*http.Client); ok {
			r.HTTPClient = c
			r.CustomHTTPDoer = nil
		} else {
			r.CustomHTTPDoer = client
		}
		return nil
	})
}

// WithMiddleware appends middleware to the request chain, in call order.
func WithMiddleware(middlewares ...Middleware) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Middlewares = append(r.Middlewares, middlewares...)
		return nil
	})
}

// WithMaxRetries sets how many times a failed request is retried. It panics
// when retries is negative.
func WithMaxRetries(retries int) RequestOption {
	if retries < 0 {
		panic("option: cannot have fewer than 0 retries")
	}
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.MaxRetries = retries
		return nil
	})
}

// WithRequestTimeout sets a per-request timeout.
func WithRequestTimeout(dur time.Duration) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.RequestTimeout = dur
		return nil
	})
}

// WithHeader sets a header, replacing any existing value.
func WithHeader(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Set(key, value)
		return nil
	})
}

// WithHeaderAdd appends a header value.
func WithHeaderAdd(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Add(key, value)
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

// WithQuery sets a query parameter, replacing any existing value.
func WithQuery(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		query := r.Request.URL.Query()
		query.Set(key, value)
		r.Request.URL.RawQuery = query.Encode()
		return nil
	})
}

// WithJSONSet sets a key in the request body. The key is a dot-separated
// path, e.g. "output_config.effort".
func WithJSONSet(key string, value any) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		body, err := setJSONPath(r.Body, strings.Split(key, "."), value)
		if err != nil {
			return err
		}
		r.Body = body
		return nil
	})
}

// WithJSONDel removes a top-level key from the request body.
func WithJSONDel(key string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if len(r.Body) == 0 {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(r.Body, &obj); err != nil {
			return err
		}
		delete(obj, key)
		body, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		r.Body = body
		return nil
	})
}

func setJSONPath(body []byte, path []string, value any) ([]byte, error) {
	members, err := parseJSONObject(body)
	if err != nil {
		return nil, fmt.Errorf("requestoption: cannot set %q on a non-object body: %w", strings.Join(path, "."), err)
	}

	idx := -1
	for i := range members {
		if members[i].key == path[0] {
			idx = i
			break
		}
	}

	var encoded []byte
	if len(path) == 1 {
		encoded, err = json.Marshal(value)
	} else {
		var nested json.RawMessage
		if idx >= 0 {
			nested = members[idx].value
		}
		encoded, err = setJSONPath(nested, path[1:], value)
	}
	if err != nil {
		return nil, err
	}

	// Existing keys keep their position and new ones are appended, matching the
	// upstream sjson behaviour the recorded cassettes were captured with.
	if idx >= 0 {
		members[idx].value = encoded
	} else {
		members = append(members, jsonMember{key: path[0], value: encoded})
	}
	return encodeJSONObject(members)
}

// jsonMember is one key/value pair of a JSON object, in document order.
type jsonMember struct {
	key   string
	value json.RawMessage
}

func parseJSONObject(body []byte) ([]jsonMember, error) {
	if len(body) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object, got %v", tok)
	}
	var members []jsonMember
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected a JSON object key, got %v", keyTok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		members = append(members, jsonMember{key: key, value: raw})
	}
	return members, nil
}

func encodeJSONObject(members []jsonMember) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(m.value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// WithAPIKey sets the API key sent in the X-Api-Key header.
func WithAPIKey(value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.APIKey = value
		return r.Apply(WithHeader("X-Api-Key", r.APIKey))
	})
}

// WithAuthToken sets the bearer token sent in the Authorization header.
func WithAuthToken(value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.AuthToken = value
		return r.Apply(WithHeader("authorization", fmt.Sprintf("Bearer %s", r.AuthToken)))
	})
}

// WithEnvironmentProduction points the client at the production API.
func WithEnvironmentProduction() RequestOption {
	return requestconfig.WithDefaultBaseURL("https://api.anthropic.com/")
}

// IdentityTokenFunc returns a fresh JWT identity token (from SPIFFE/SPIRE, a
// cloud metadata server, or any other OIDC-compatible provider). It is called
// once per token exchange and must be safe for concurrent use.
type IdentityTokenFunc func(ctx context.Context) (string, error)

// FederationOptions configures a workload-identity-federation token exchange.
type FederationOptions struct {
	// FederationRuleID identifies the OidcFederationRule governing this
	// exchange. Required; a tagged ID with the "fdrl_" prefix.
	FederationRuleID string
	// OrganizationID is the UUID of the Anthropic organization. Required.
	OrganizationID string
	// ServiceAccountID is an optional expected-target check ("svac_" prefix).
	ServiceAccountID string
	// WorkspaceID optionally scopes the minted token to one workspace.
	WorkspaceID string
}

// tokenEndpoint is the OAuth token-exchange path, relative to the base URL.
const tokenEndpoint = "v1/oauth/token"

const (
	grantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	// oauthBetaHeader unlocks the token endpoint family; federationBetaHeader
	// unlocks jwt-bearer grants. Both are required on the exchange request.
	oauthBetaHeader      = "oauth-2025-04-20"
	federationBetaHeader = "workload-identity-federation-2025-11-11"
	// maxAssertionSize bounds the JWT sent to the token endpoint. Honest OIDC
	// tokens are far below this.
	maxAssertionSize = 16384
	// refreshThreshold re-exchanges a token shortly before it expires.
	refreshThreshold = 120 * time.Second
)

// WithFederationTokenProvider authenticates requests using workload identity
// federation: provider's JWT is exchanged for a short-lived Anthropic access
// token, which is cached until it approaches expiry and sent as a bearer
// token.
func WithFederationTokenProvider(provider IdentityTokenFunc, opts FederationOptions) RequestOption {
	switch {
	case provider == nil:
		return errOption(fmt.Errorf("option: WithFederationTokenProvider: provider is nil"))
	case opts.FederationRuleID == "":
		return errOption(fmt.Errorf("option: WithFederationTokenProvider: FederationRuleID is required"))
	case opts.OrganizationID == "":
		return errOption(fmt.Errorf("option: WithFederationTokenProvider: OrganizationID is required"))
	}

	cache := &federationTokenCache{provider: provider, opts: opts}
	return WithMiddleware(cache.middleware)
}

func errOption(err error) RequestOption {
	return requestconfig.RequestOptionFunc(func(*requestconfig.RequestConfig) error { return err })
}

type federationTokenCache struct {
	provider IdentityTokenFunc
	opts     FederationOptions

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (c *federationTokenCache) middleware(req *http.Request, next MiddlewareNext) (*http.Response, error) {
	token, err := c.accessToken(req, next)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Del("X-Api-Key")
	req.Header.Add("anthropic-beta", oauthBetaHeader)
	return next(req)
}

func (c *federationTokenCache) accessToken(req *http.Request, next MiddlewareNext) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && (c.expiresAt.IsZero() || time.Until(c.expiresAt) > refreshThreshold) {
		return c.token, nil
	}

	ctx := req.Context()
	jwt, err := c.provider(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get identity token: %w", err)
	}
	if len(jwt) > maxAssertionSize {
		return "", fmt.Errorf("identity token exceeds %d-byte limit (got %d bytes)", maxAssertionSize, len(jwt))
	}

	endpoint := *req.URL
	base := req.URL.Path
	if i := strings.Index(base, "/v1/"); i >= 0 {
		base = base[:i+1]
	}
	endpoint.Path = base + tokenEndpoint
	endpoint.RawQuery = ""
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return "", fmt.Errorf("federation token endpoint must be https, got %q", endpoint.String())
	}

	payload, err := json.Marshal(map[string]string{
		"grant_type":         grantTypeJWTBearer,
		"assertion":          jwt,
		"federation_rule_id": c.opts.FederationRuleID,
		"organization_id":    c.opts.OrganizationID,
		"service_account_id": c.opts.ServiceAccountID,
		"workspace_id":       c.opts.WorkspaceID,
	})
	if err != nil {
		return "", err
	}

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("failed to create token exchange request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("anthropic-beta", oauthBetaHeader+","+federationBetaHeader)

	resp, err := next(tokenReq)
	if err != nil {
		return "", fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   *int   `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse token exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("token exchange response missing access_token")
	}
	if result.TokenType != "" && !strings.EqualFold(result.TokenType, "Bearer") {
		return "", fmt.Errorf("token exchange response: unsupported token_type %q (want Bearer)", result.TokenType)
	}

	c.token = result.AccessToken
	c.expiresAt = time.Time{}
	if result.ExpiresIn != nil {
		c.expiresAt = time.Now().Add(time.Duration(*result.ExpiresIn) * time.Second)
	}
	return c.token, nil
}

// Package cloudauth implements the RFC 8628 device authorization flow used
// to authenticate docker-agent against the Agentic Platform (AP).
//
// Communication with AP is done over plain HTTP+JSON (Connect unary protocol
// is JSON over POST), so this package intentionally does NOT depend on the
// AP-generated protos.
package cloudauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker-agent/pkg/paths"
)

// DefaultEndpoint is the default AP base URL used when no override is set.
const DefaultEndpoint = "https://agentic-platform-stage.docker.com"

// AppID identifies docker-agent in the AP device-flow registry.
const AppID = "docker-agent"

// CredentialsFileName is the on-disk name (under the cagent config dir).
const CredentialsFileName = "credentials.json"

// Credentials is the on-disk representation of a successful login.
type Credentials struct {
	APEndpoint   string    `json:"ap_endpoint"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id,omitempty"`
	UserEmail    string    `json:"user_email,omitempty"`
}

// CredentialsPath returns the absolute path to the credentials file.
func CredentialsPath() string {
	return filepath.Join(paths.GetConfigDir(), CredentialsFileName)
}

// LoadCredentials reads credentials.json from disk. Returns os.ErrNotExist
// when no credentials are present.
func LoadCredentials() (*Credentials, error) {
	raw, err := os.ReadFile(CredentialsPath())
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return &c, nil
}

// SaveCredentials writes credentials.json with mode 0600.
func SaveCredentials(c *Credentials) error {
	if err := os.MkdirAll(paths.GetConfigDir(), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(CredentialsPath(), raw, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

// Logout removes the credentials file. Missing file is not an error.
func Logout(_ context.Context) error {
	err := os.Remove(CredentialsPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// StartDeviceAuthorizationResponse mirrors the AP RPC response.
//
// AP serves Connect unary JSON with proto3-canonical (camelCase) field names,
// so these JSON tags must match the camelCase form — not the proto snake_case
// original. Mixing the two silently zero-values fields on decode.
type StartDeviceAuthorizationResponse struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	// VerificationURIComplete is verification_uri with user_code pre-filled.
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// PollDeviceTokenResponse mirrors the AP RPC response.
type PollDeviceTokenResponse struct {
	// Pending is true while the user has not yet approved the device.
	Pending bool `json:"pending,omitempty"`
	// Status is set by some AP versions instead of (or alongside) Pending.
	// Known values: "pending", "approved", "denied", "expired".
	Status       string `json:"status,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	TokenType    string `json:"tokenType,omitempty"`
	ExpiresIn    int    `json:"expiresIn,omitempty"`
	UserID       string `json:"userId,omitempty"`
	UserEmail    string `json:"userEmail,omitempty"`
}

// DeviceAuth is a low-level driver that performs the device-flow RPCs against
// the given AP base URL.
type DeviceAuth struct {
	Endpoint string
	Client   *http.Client
}

// New returns a DeviceAuth configured against endpoint (or DefaultEndpoint).
func New(endpoint string) *DeviceAuth {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &DeviceAuth{
		Endpoint: endpoint,
		Client:   HTTPClient(endpoint),
	}
}

// Start initiates the device-code flow.
func (d *DeviceAuth) Start(ctx context.Context) (*StartDeviceAuthorizationResponse, error) {
	body := map[string]string{"appId": AppID}
	var resp StartDeviceAuthorizationResponse
	if err := d.call(ctx, "/api/auth.v1.AuthService/StartDeviceAuthorization", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Poll polls once for a token using the supplied device_code.
func (d *DeviceAuth) Poll(ctx context.Context, deviceCode string) (*PollDeviceTokenResponse, error) {
	body := map[string]string{"deviceCode": deviceCode, "appId": AppID}
	var resp PollDeviceTokenResponse
	if err := d.call(ctx, "/api/auth.v1.AuthService/PollDeviceToken", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (d *DeviceAuth) call(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("ap call %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ap call %s: HTTP %d: %s", path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// PromptFunc is invoked once the device-code is issued so the caller can
// display the user_code / verification_uri to the user. Implementations must
// return quickly — polling continues regardless.
type PromptFunc func(userCode, verificationURI, verificationURIComplete string)

// LoginOption customizes the Login call.
type LoginOption func(*loginOptions)

type loginOptions struct {
	prompt PromptFunc
}

// WithPrompt installs a PromptFunc that is called once the device code has
// been issued. Use this to surface the user_code/verification_uri in the TUI.
func WithPrompt(fn PromptFunc) LoginOption {
	return func(o *loginOptions) { o.prompt = fn }
}

// Login runs the full device-flow: Start, surface user_code, poll until
// approved/denied/expired, persist the credentials.
func Login(ctx context.Context, endpoint string, opts ...LoginOption) (*Credentials, error) {
	options := &loginOptions{}
	for _, o := range opts {
		o(options)
	}

	d := New(endpoint)
	start, err := d.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start device authorization: %w", err)
	}

	if options.prompt != nil {
		options.prompt(start.UserCode, start.VerificationURI, start.VerificationURIComplete)
	}

	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expires := time.Duration(start.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	deadline := time.Now().Add(expires)

	for {
		if time.Now().After(deadline) {
			return nil, errors.New("device authorization expired")
		}
		// Sleep first to give the user a chance to approve, but allow ctx cancel.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		poll, err := d.Poll(ctx, start.DeviceCode)
		if err != nil {
			return nil, fmt.Errorf("poll device token: %w", err)
		}

		switch poll.Status {
		case "approved", "":
			// AP signals "still waiting" two different ways across versions:
			//   - { "pending": true } with no other fields, or
			//   - { "status": "pending" }.
			// On approval, AP returns access_token (with no status field). Treat
			// presence of access_token as the authoritative success signal.
			if poll.Pending || poll.AccessToken == "" {
				continue
			}
			creds := &Credentials{
				APEndpoint:   d.Endpoint,
				AccessToken:  poll.AccessToken,
				RefreshToken: poll.RefreshToken,
				TokenType:    defaultTokenType(poll.TokenType),
				ExpiresAt:    time.Now().Add(time.Duration(poll.ExpiresIn) * time.Second),
				UserID:       poll.UserID,
				UserEmail:    poll.UserEmail,
			}
			if err := SaveCredentials(creds); err != nil {
				return nil, err
			}
			return creds, nil
		case "pending":
			continue
		case "denied":
			return nil, errors.New("device authorization denied by user")
		case "expired":
			return nil, errors.New("device authorization expired")
		default:
			// Unknown status — keep polling defensively.
			continue
		}
	}
}

func defaultTokenType(t string) string {
	if t == "" {
		return "Bearer"
	}
	return t
}

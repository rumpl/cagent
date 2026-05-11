// Package cloudbridge plumbs docker-agent into the Agentic Platform (AP)
// "Local Agent" service. It mirrors local sessions to AP and pulls
// remote prompts addressed to those sessions.
//
// All AP traffic is plain JSON over HTTP (Connect unary protocol), so this
// package intentionally does not depend on the AP-generated protos.
package cloudbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/cloudauth"
)

// httpClientFor returns the HTTP client used for AP calls to base. It honours
// CAGENT_CLOUD_INSECURE and auto-trusts localhost endpoints so local
// development against an AP backend with a self-signed certificate works.
func httpClientFor(base string) *http.Client {
	c := cloudauth.HTTPClient(base)
	c.Timeout = 60 * time.Second
	return c
}

// refreshMu serialises credentials refresh so concurrent 401s only spawn one
// RefreshToken call.
var refreshMu sync.Mutex

// CallAP performs a Connect unary JSON POST against the AP base URL stored in
// the local credentials file.
//
// On HTTP 401 it attempts a single RefreshToken round-trip, updates the
// credentials file in-place, and retries the original request once.
//
// req may be nil (sent as "{}"). resp may be nil (response body discarded).
func CallAP(ctx context.Context, endpoint, method string, req, resp any) error {
	creds, err := cloudauth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	base := endpoint
	if base == "" {
		base = creds.APEndpoint
	}
	if base == "" {
		base = cloudauth.DefaultEndpoint
	}

	status, body, err := doCall(ctx, base, method, creds.AccessToken, req)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		// Try refresh, then retry once.
		if rerr := refreshCredentials(ctx, base, creds); rerr != nil {
			return fmt.Errorf("refresh credentials: %w", rerr)
		}
		// Re-read latest creds.
		creds, err = cloudauth.LoadCredentials()
		if err != nil {
			return fmt.Errorf("reload credentials: %w", err)
		}
		status, body, err = doCall(ctx, base, method, creds.AccessToken, req)
		if err != nil {
			return err
		}
	}
	if status/100 != 2 {
		return newAPError(method, status, body)
	}
	if resp == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, resp); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}

// doCall performs one POST. Returns status, body, transport error.
func doCall(ctx context.Context, base, method, token string, req any) (int, []byte, error) {
	var payload []byte
	if req != nil {
		var err error
		payload, err = json.Marshal(req)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal request: %w", err)
		}
	} else {
		payload = []byte("{}")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+method, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	httpResp, err := httpClientFor(base).Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("ap %s: %w", method, err)
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("ap %s: read body: %w", method, err)
	}
	return httpResp.StatusCode, data, nil
}

// refreshCredentials calls AP's RefreshToken and persists the new credentials.
func refreshCredentials(ctx context.Context, base string, prev *cloudauth.Credentials) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	// Re-read in case a concurrent caller already refreshed.
	if cur, err := cloudauth.LoadCredentials(); err == nil && cur.AccessToken != prev.AccessToken {
		return nil
	}

	if prev.RefreshToken == "" {
		return errors.New("no refresh_token available")
	}

	body := map[string]string{"refreshToken": prev.RefreshToken}
	status, raw, err := doCall(ctx, base, "/api/auth.v1.AuthService/RefreshToken", "", body)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return fmt.Errorf("refresh_token: HTTP %d: %s", status, string(raw))
	}
	var resp struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		TokenType    string `json:"tokenType"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode refresh_token response: %w", err)
	}
	if resp.AccessToken == "" {
		return errors.New("refresh_token: empty access_token in response")
	}

	updated := *prev
	updated.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		updated.RefreshToken = resp.RefreshToken
	}
	if resp.TokenType != "" {
		updated.TokenType = resp.TokenType
	}
	if resp.ExpiresIn > 0 {
		updated.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	if err := cloudauth.SaveCredentials(&updated); err != nil {
		return fmt.Errorf("persist refreshed credentials: %w", err)
	}
	slog.Debug("cloudbridge: refreshed AP access token")
	return nil
}

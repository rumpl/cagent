package cloudauth

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// InsecureEnvVar, when set to a truthy value, forces all AP HTTP calls to
// skip TLS certificate verification. Intended for local development against
// an AP backend serving a self-signed cert (e.g. via Caddy).
const InsecureEnvVar = "CAGENT_CLOUD_INSECURE"

// HTTPClient returns an *http.Client suitable for talking to the given AP
// endpoint. The returned client skips TLS verification when:
//
//   - CAGENT_CLOUD_INSECURE is set to a truthy value, or
//   - endpoint targets localhost / 127.0.0.1 / ::1 over https
//
// Both cases cover local development where AP is served behind a Caddy
// self-signed certificate.
func HTTPClient(endpoint string) *http.Client {
	timeout := 30 * time.Second
	if !insecureFor(endpoint) {
		return &http.Client{Timeout: timeout}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in localhost/dev only
	return &http.Client{Timeout: timeout, Transport: tr}
}

func insecureFor(endpoint string) bool {
	if v, err := strconv.ParseBool(os.Getenv(InsecureEnvVar)); err == nil && v {
		return true
	}
	if endpoint == "" {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

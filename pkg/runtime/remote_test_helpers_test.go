package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// httptest.Server.Close closes the global pool, so parallel tests need private transports.
func newTestTransport(t *testing.T) *http.Transport {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	return transport
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	opts := []ClientOption{WithHTTPClient(&http.Client{
		Transport: newTestTransport(t),
		Timeout:   30 * time.Second,
	})}
	client, err := NewClient(baseURL, opts...)
	require.NoError(t, err)
	return client
}

package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/remote"
)

// httptest.Server.Close closes the global pool, so parallel tests need private transports.
func newTestTransport(t *testing.T) *http.Transport {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	return transport
}

func newTestClient(t *testing.T, baseURL string) *remote.Client {
	t.Helper()
	opts := []remote.ClientOption{remote.WithHTTPClient(&http.Client{
		Transport: newTestTransport(t),
		Timeout:   30 * time.Second,
	})}
	client, err := remote.NewClient(baseURL, opts...)
	require.NoError(t, err)
	return client
}

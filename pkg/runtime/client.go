package runtime

import (
	"net/http"
	"time"

	"github.com/docker/docker-agent/pkg/runtime/remote"
)

// Client is the HTTP remote-runtime client.
type Client = remote.Client

// ClientOption configures a remote HTTP client.
type ClientOption = remote.ClientOption

// ErrorResponse is the remote API error payload.
type ErrorResponse = remote.ErrorResponse

func NewClient(url string, opts ...ClientOption) (*Client, error) {
	return remote.NewClient(url, opts...)
}
func WithHTTPClient(c *http.Client) ClientOption     { return remote.WithHTTPClient(c) }
func WithAuthToken(token string) ClientOption        { return remote.WithAuthToken(token) }
func WithTimeout(timeout time.Duration) ClientOption { return remote.WithTimeout(timeout) }

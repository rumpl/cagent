package auth

import (
	"context"

	"github.com/docker/cagent/pkg/environment"
)

// Provider abstracts how providers obtain an auth token.
//
// Implementations may read from environment variables, OS keychains,
// Docker Desktop, etc. Implementations are expected to be safe to call
// frequently (e.g. once per request) because some tokens are short-lived.
type Provider interface {
	Token(ctx context.Context, env environment.Provider) (string, error)
}

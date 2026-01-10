package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/environment"
)

type DockerDesktopTokenProvider struct{}

func NewDockerDesktopTokenProvider() *DockerDesktopTokenProvider {
	return &DockerDesktopTokenProvider{}
}

func (p *DockerDesktopTokenProvider) Token(ctx context.Context, env environment.Provider) (string, error) {
	if env == nil {
		return "", errors.New("environment provider is required")
	}
	// Always ask Docker Desktop for the freshest token.
	// We allow a deterministic env-var fallback for tests and legacy setups.
	// Support deterministic testing by allowing an env-var override.
	// In production, Docker Desktop should be the source of truth.
	if v, ok := env.Get(ctx, environment.DockerDesktopTokenEnv); ok {
		v = strings.TrimSpace(v)
		if v == "" {
			return "", errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}
		return v, nil
	}

	token := strings.TrimSpace(desktop.GetToken(ctx))
	if token == "" {
		return "", errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
	}
	return token, nil
}

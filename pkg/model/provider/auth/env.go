package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/environment"
)

type EnvTokenProvider struct {
	EnvVar string
}

func NewEnvTokenProvider(envVar string) *EnvTokenProvider {
	return &EnvTokenProvider{EnvVar: envVar}
}

func (p *EnvTokenProvider) Token(ctx context.Context, env environment.Provider) (string, error) {
	if env == nil {
		return "", fmt.Errorf("environment provider is required")
	}
	name := strings.TrimSpace(p.EnvVar)
	if name == "" {
		return "", fmt.Errorf("env var name is required")
	}
	v, _ := env.Get(ctx, name)
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s environment variable is required", name)
	}
	return v, nil
}

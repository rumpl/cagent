package auth

import (
	"context"

	"github.com/docker/cagent/pkg/environment"
)

type NoopProvider struct{}

func NewNoopProvider() *NoopProvider {
	return &NoopProvider{}
}

func (p *NoopProvider) Token(ctx context.Context, env environment.Provider) (string, error) {
	_ = ctx
	_ = env
	return "", nil
}

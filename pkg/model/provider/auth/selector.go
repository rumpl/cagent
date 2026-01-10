package auth

import (
	"context"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// ProviderFor returns the auth Provider to use for a given model config.
//
// Selection rules:
//   - If token_key is set, use an EnvTokenProvider for that env var.
//   - If a gateway is configured, use Docker Desktop token provider.
//   - For Google/VertexAI configs, allow running without an API key.
//   - Otherwise return a NoopProvider and let provider-specific logic decide.
func ProviderFor(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts options.ModelOptions) Provider {
	_ = ctx
	_ = env
	if cfg == nil {
		return nil
	}

	if cfg.TokenKey != "" {
		return NewEnvTokenProvider(cfg.TokenKey)
	}

	// If we're using a models gateway, prefer Docker Desktop auth.
	// This keeps e2e (which routes everything through the proxy gateway) from
	// requiring provider-specific API keys like MISTRAL_API_KEY.
	if opts.Gateway() != "" {
		// Only use Docker Desktop auth when a desktop token is available.
		// Gateways like the e2e proxy don't need or use the real provider API keys.
		return NewDockerDesktopTokenProvider()
	}

	if cfg.Provider == "google" {
		if cfg.ProviderOpts["project"] != nil || cfg.ProviderOpts["location"] != nil {
			return NewNoopProvider()
		}
		return NewEnvTokenProvider("GOOGLE_API_KEY")
	}

	switch cfg.Provider {
	case "anthropic":
		return NewEnvTokenProvider("ANTHROPIC_API_KEY")
	default:
		return NewNoopProvider()
	}
}

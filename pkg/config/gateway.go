package config

import "github.com/docker/cagent/pkg/config/latest"

// ResolveModelsGateway returns the effective models gateway URL.
//
// Gateway can be configured either via runtime config (RuntimeConfig.ModelsGateway)
// or by defining a provider named "gateway" with a base_url.
//
// If both are set, runtimeModelsGateway wins.
func ResolveModelsGateway(cfg *latest.Config, runtimeModelsGateway string) string {
	if runtimeModelsGateway != "" {
		return runtimeModelsGateway
	}
	if cfg == nil || cfg.Providers == nil {
		return ""
	}
	if p, ok := cfg.Providers["gateway"]; ok {
		return p.BaseURL
	}
	return ""
}

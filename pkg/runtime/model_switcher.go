package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// ModelChoice represents a model available for selection in the TUI picker.
type ModelChoice struct {
	// Name is the display name (config key)
	Name string
	// Ref is the model reference used internally (e.g., "my_model" or "openai/gpt-4o")
	Ref string
	// Provider is the provider name (e.g., "openai", "anthropic")
	Provider string
	// Model is the specific model name (e.g., "gpt-4o", "claude-sonnet-4-0")
	Model string
	// IsDefault indicates this is the agent's configured default model
	IsDefault bool
	// IsCurrent indicates this is the currently active model for the agent
	IsCurrent bool
	// IsCustom indicates this is a custom model from the session history (not from config)
	IsCustom bool
}

// ModelSwitcher is an optional interface for runtimes that support changing the model
// for the current agent at runtime. This is used by the TUI for model switching.
type ModelSwitcher interface {
	// SetAgentModel sets a model override for the specified agent.
	// modelRef can be:
	// - "" (empty) to clear the override and use the agent's default model
	// - A model name from the config (e.g., "my_fast_model")
	// - An inline model spec (e.g., "openai/gpt-4o")
	SetAgentModel(ctx context.Context, agentName, modelRef string) error

	// AvailableModels returns the list of models available for selection.
	// This includes all models defined in the config, with the current agent's
	// default model marked as IsDefault.
	AvailableModels(ctx context.Context) []ModelChoice
}

// ModelSwitcherConfig holds the configuration needed for model switching.
// This is populated by the app layer when creating the runtime.
type ModelSwitcherConfig struct {
	// Models is the map of model names to configurations from the loaded config
	Models map[string]latest.ModelConfig
	// Providers is the map of custom provider configurations
	Providers map[string]latest.ProviderConfig
	// ModelsGateway is the gateway URL if configured
	ModelsGateway string
	// EnvProvider provides access to environment variables
	EnvProvider environment.Provider
	// AgentDefaultModels maps agent names to their configured default model references
	AgentDefaultModels map[string]string
}

// SetAgentModel implements ModelSwitcher for LocalRuntime.
func (r *LocalRuntime) SetAgentModel(ctx context.Context, agentName, modelRef string) error {
	if r.modelSwitcherCfg == nil {
		return fmt.Errorf("model switching not configured for this runtime")
	}

	a, err := r.team.Agent(agentName)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	// Empty modelRef means clear the override (use agent's default)
	if modelRef == "" {
		a.SetModelOverride()
		slog.Info("Cleared agent model override (using default)", "agent", agentName)
		return nil
	}

	// Check if modelRef is a named model from config
	if modelConfig, exists := r.modelSwitcherCfg.Models[modelRef]; exists {
		// Check if this is an alloy model (no provider, comma-separated models)
		if isAlloyModelConfig(modelConfig) {
			providers, err := r.createProvidersFromAlloyConfig(ctx, modelConfig)
			if err != nil {
				return fmt.Errorf("failed to create alloy model from config: %w", err)
			}
			a.SetModelOverride(providers...)
			slog.Info("Set agent model override (alloy)", "agent", agentName, "config_name", modelRef, "model_count", len(providers))
			return nil
		}

		prov, err := r.createProviderFromConfig(ctx, &modelConfig)
		if err != nil {
			return fmt.Errorf("failed to create model from config: %w", err)
		}
		a.SetModelOverride(prov)
		slog.Info("Set agent model override", "agent", agentName, "model", prov.ID(), "config_name", modelRef)
		return nil
	}

	// Check if this is an inline alloy spec (comma-separated provider/model specs)
	// e.g., "openai/gpt-4o,anthropic/claude-sonnet-4-0"
	if isInlineAlloySpec(modelRef) {
		providers, err := r.createProvidersFromInlineAlloy(ctx, modelRef)
		if err != nil {
			return fmt.Errorf("failed to create inline alloy model: %w", err)
		}
		a.SetModelOverride(providers...)
		slog.Info("Set agent model override (inline alloy)", "agent", agentName, "model_count", len(providers))
		return nil
	}

	// Try parsing as inline spec (provider/model)
	providerName, modelName, ok := strings.Cut(modelRef, "/")
	if !ok {
		return fmt.Errorf("invalid model reference %q: expected a model name from config or 'provider/model' format", modelRef)
	}

	inlineCfg := &latest.ModelConfig{
		Provider: providerName,
		Model:    modelName,
	}
	prov, err := r.createProviderFromConfig(ctx, inlineCfg)
	if err != nil {
		return fmt.Errorf("failed to create inline model: %w", err)
	}
	a.SetModelOverride(prov)
	slog.Info("Set agent model override (inline)", "agent", agentName, "model", prov.ID())
	return nil
}

// isAlloyModelConfig checks if a model config is an alloy model (multiple models).
func isAlloyModelConfig(cfg latest.ModelConfig) bool {
	return cfg.Provider == "" && strings.Contains(cfg.Model, ",")
}

// isInlineAlloySpec checks if a model reference is an inline alloy specification.
// An inline alloy is comma-separated provider/model specs like "openai/gpt-4o,anthropic/claude-sonnet-4-0".
func isInlineAlloySpec(modelRef string) bool {
	if !strings.Contains(modelRef, ",") {
		return false
	}
	// Check that each part looks like a provider/model spec
	// and count valid parts (need at least 2 for an alloy)
	validParts := 0
	for part := range strings.SplitSeq(modelRef, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			return false
		}
		validParts++
	}
	return validParts >= 2
}

// createProvidersFromInlineAlloy creates providers from an inline alloy spec.
// An inline alloy is comma-separated provider/model specs like "openai/gpt-4o,anthropic/claude-sonnet-4-0".
func (r *LocalRuntime) createProvidersFromInlineAlloy(ctx context.Context, modelRef string) ([]provider.Provider, error) {
	var providers []provider.Provider

	for part := range strings.SplitSeq(modelRef, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this part exists as a named model in config
		if modelCfg, exists := r.modelSwitcherCfg.Models[part]; exists {
			prov, err := r.createProviderFromConfig(ctx, &modelCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to create provider for %q: %w", part, err)
			}
			providers = append(providers, prov)
			continue
		}

		// Parse as provider/model
		providerName, modelName, ok := strings.Cut(part, "/")
		if !ok {
			return nil, fmt.Errorf("invalid model reference %q in inline alloy: expected 'provider/model' format", part)
		}

		inlineCfg := &latest.ModelConfig{
			Provider: providerName,
			Model:    modelName,
		}
		prov, err := r.createProviderFromConfig(ctx, inlineCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider for %q: %w", part, err)
		}
		providers = append(providers, prov)
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("inline alloy spec has no valid models")
	}

	return providers, nil
}

// createProvidersFromAlloyConfig creates providers for each model in an alloy configuration.
func (r *LocalRuntime) createProvidersFromAlloyConfig(ctx context.Context, alloyCfg latest.ModelConfig) ([]provider.Provider, error) {
	var providers []provider.Provider

	for modelRef := range strings.SplitSeq(alloyCfg.Model, ",") {
		modelRef = strings.TrimSpace(modelRef)
		if modelRef == "" {
			continue
		}

		// Check if this model reference exists in the config
		if modelCfg, exists := r.modelSwitcherCfg.Models[modelRef]; exists {
			prov, err := r.createProviderFromConfig(ctx, &modelCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to create provider for %q: %w", modelRef, err)
			}
			providers = append(providers, prov)
			continue
		}

		// Try parsing as inline spec (provider/model)
		providerName, modelName, ok := strings.Cut(modelRef, "/")
		if !ok {
			return nil, fmt.Errorf("invalid model reference %q in alloy config: expected 'provider/model' format", modelRef)
		}

		inlineCfg := &latest.ModelConfig{
			Provider: providerName,
			Model:    modelName,
		}
		prov, err := r.createProviderFromConfig(ctx, inlineCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider for %q: %w", modelRef, err)
		}
		providers = append(providers, prov)
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("alloy model config has no valid models")
	}

	return providers, nil
}

// AvailableModels implements ModelSwitcher for LocalRuntime.
func (r *LocalRuntime) AvailableModels(_ context.Context) []ModelChoice {
	var choices []ModelChoice

	if r.modelSwitcherCfg == nil {
		return choices
	}

	// Get the current agent's default model reference
	currentAgentDefault := ""
	if r.modelSwitcherCfg.AgentDefaultModels != nil {
		currentAgentDefault = r.modelSwitcherCfg.AgentDefaultModels[r.currentAgent]
	}

	// Add all configured models, marking the current agent's default
	for name, cfg := range r.modelSwitcherCfg.Models {
		choices = append(choices, ModelChoice{
			Name:      name,
			Ref:       name,
			Provider:  cfg.Provider,
			Model:     cfg.Model,
			IsDefault: name == currentAgentDefault,
		})
	}

	return choices
}

// createProviderFromConfig creates a provider from a ModelConfig using the runtime's configuration.
func (r *LocalRuntime) createProviderFromConfig(ctx context.Context, cfg *latest.ModelConfig) (provider.Provider, error) {
	opts := []options.Opt{
		options.WithGateway(config.ResolveModelsGateway(&latest.Config{Providers: r.modelSwitcherCfg.Providers}, r.modelSwitcherCfg.ModelsGateway)),
		options.WithProviders(r.modelSwitcherCfg.Providers),
	}

	// Look up max tokens from models.dev if not specified in config
	var maxTokens *int64
	if cfg.MaxTokens != nil {
		maxTokens = cfg.MaxTokens
	} else {
		defaultMaxTokens := int64(32000)
		maxTokens = &defaultMaxTokens
		if r.modelsStore != nil {
			m, err := r.modelsStore.GetModel(ctx, cfg.Provider+"/"+cfg.Model)
			if err == nil && m != nil {
				maxTokens = &m.Limit.Output
			}
		}
	}
	if maxTokens != nil {
		opts = append(opts, options.WithMaxTokens(*maxTokens))
	}

	return provider.NewWithModels(ctx,
		cfg,
		r.modelSwitcherCfg.Models,
		r.modelSwitcherCfg.EnvProvider,
		opts...,
	)
}

// WithModelSwitcherConfig sets the model switcher configuration for the runtime.
func WithModelSwitcherConfig(cfg *ModelSwitcherConfig) Opt {
	return func(r *LocalRuntime) {
		r.modelSwitcherCfg = cfg
	}
}

// Ensure LocalRuntime implements ModelSwitcher
var _ ModelSwitcher = (*LocalRuntime)(nil)

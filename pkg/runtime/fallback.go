package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/backoff"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelerrors"
	"github.com/docker/docker-agent/pkg/modelsdev"
)

// fallbackCooldownState tracks when we should stick with a fallback model
// instead of retrying the primary after a non-retryable error (e.g., 429).
type fallbackCooldownState struct {
	// fallbackIndex is the index in the fallback chain to start from (0 = first fallback, -1 = primary)
	fallbackIndex int
	// until is when the cooldown expires and we should retry the primary
	until time.Time
}

// modelWithFallback holds a provider and its identification for logging
type modelWithFallback struct {
	provider   provider.Provider
	isFallback bool
	index      int // index in fallback list (-1 for primary)
}

// buildModelChain returns the ordered list of models to try: primary first, then fallbacks.
func buildModelChain(primary provider.Provider, fallbacks []provider.Provider) []modelWithFallback {
	chain := make([]modelWithFallback, 0, 1+len(fallbacks))
	chain = append(chain, modelWithFallback{
		provider:   primary,
		isFallback: false,
		index:      -1,
	})
	for i, fb := range fallbacks {
		chain = append(chain, modelWithFallback{
			provider:   fb,
			isFallback: true,
			index:      i,
		})
	}
	return chain
}

// logFallbackAttempt logs information about a fallback attempt
func logFallbackAttempt(agentName string, model modelWithFallback, attempt, maxRetries int, err error) {
	if model.isFallback {
		slog.Warn("Fallback model attempt",
			"agent", agentName,
			"model", model.provider.ID(),
			"fallback_index", model.index,
			"attempt", attempt+1,
			"max_retries", maxRetries+1,
			"previous_error", err)
	} else {
		slog.Warn("Primary model failed, trying fallbacks",
			"agent", agentName,
			"model", model.provider.ID(),
			"error", err)
	}
}

// logRetryBackoff logs when we're backing off before a retry
func logRetryBackoff(agentName, modelID string, attempt int, backoffDelay time.Duration) {
	slog.Debug("Backing off before retry",
		"agent", agentName,
		"model", modelID,
		"attempt", attempt+1,
		"backoff", backoffDelay)
}

// getCooldownState returns the current cooldown state for an agent (thread-safe).
// Returns nil if no cooldown is active or if cooldown has expired.
// Expired entries are evicted to prevent stale state accumulation.
func (r *LocalRuntime) getCooldownState(agentName string) *fallbackCooldownState {
	r.fallbackCooldownsMux.Lock()
	defer r.fallbackCooldownsMux.Unlock()

	state := r.fallbackCooldowns[agentName]
	if state == nil {
		return nil
	}

	// Check if cooldown has expired; evict if so
	if time.Now().After(state.until) {
		delete(r.fallbackCooldowns, agentName)
		return nil
	}

	return state
}

// setCooldownState sets the cooldown state for an agent (thread-safe).
func (r *LocalRuntime) setCooldownState(agentName string, fallbackIndex int, cooldownDuration time.Duration) {
	r.fallbackCooldownsMux.Lock()
	defer r.fallbackCooldownsMux.Unlock()

	r.fallbackCooldowns[agentName] = &fallbackCooldownState{
		fallbackIndex: fallbackIndex,
		until:         time.Now().Add(cooldownDuration),
	}

	slog.Info("Fallback cooldown activated",
		"agent", agentName,
		"fallback_index", fallbackIndex,
		"cooldown", cooldownDuration,
		"until", r.fallbackCooldowns[agentName].until.Format(time.RFC3339))
}

// clearCooldownState clears the cooldown state for an agent (thread-safe).
func (r *LocalRuntime) clearCooldownState(agentName string) {
	r.fallbackCooldownsMux.Lock()
	defer r.fallbackCooldownsMux.Unlock()

	if _, exists := r.fallbackCooldowns[agentName]; exists {
		delete(r.fallbackCooldowns, agentName)
		slog.Debug("Fallback cooldown cleared", "agent", agentName)
	}
}

// getEffectiveCooldown returns the cooldown duration to use for an agent.
// Uses the agent's configured cooldown, or the default if not set.
func getEffectiveCooldown(a *agent.Agent) time.Duration {
	cooldown := a.FallbackCooldown()
	if cooldown == 0 {
		return modelerrors.DefaultCooldown
	}
	return cooldown
}

// getEffectiveRetries returns the number of retries to use for the agent.
// If no retries are explicitly configured (retries == 0), returns
// the default to provide sensible retry behavior out of the box.
// This ensures that transient errors (e.g., Anthropic 529 overloaded) are
// retried even when no fallback models are configured.
//
// Note: Users who explicitly want 0 retries can set retries: -1 in their config
// (though this is an edge case - most users want some retries for resilience).
func getEffectiveRetries(a *agent.Agent) int {
	retries := a.FallbackRetries()
	// -1 means "explicitly no retries" (workaround for Go's zero value)
	if retries < 0 {
		return 0
	}
	// 0 means "use default" - always provide retries for transient error resilience
	if retries == 0 {
		return modelerrors.DefaultRetries
	}
	return retries
}

// fallbackModelMiddleware retries the current model and falls back to the
// agent's configured alternates without baking that policy into the execution
// loop itself.
func (r *LocalRuntime) fallbackModelMiddleware() ModelMiddleware {
	return func(ctx context.Context, phase *ModelPhase, next ModelHandler) (*ModelResult, error) {
		return r.tryModelWithFallback(ctx, phase, next)
	}
}

// tryModelWithFallback executes the model phase via next, retrying the current
// model and walking the fallback chain when needed.
func (r *LocalRuntime) tryModelWithFallback(ctx context.Context, phase *ModelPhase, next ModelHandler) (*ModelResult, error) {
	a := phase.Agent
	primaryModel := phase.Model
	fallbackModels := a.FallbackModels()
	fallbackRetries := getEffectiveRetries(a)

	// Build the chain of models to try: primary (index 0) + fallbacks (index 1+).
	modelChain := buildModelChain(primaryModel, fallbackModels)

	// Check if we're in a cooldown period and should skip the primary.
	startIndex := 0
	inCooldown := false
	cooldownState := r.getCooldownState(a.Name())
	if cooldownState != nil && len(fallbackModels) > cooldownState.fallbackIndex {
		startIndex = cooldownState.fallbackIndex + 1 // +1 because index 0 is primary
		inCooldown = true
		slog.Debug("Skipping primary due to cooldown",
			"agent", a.Name(),
			"start_from_fallback_index", cooldownState.fallbackIndex,
			"cooldown_until", cooldownState.until.Format(time.RFC3339))
	}

	var lastErr error
	primaryFailedWithNonRetryable := false
	hasFallbacks := len(fallbackModels) > 0

	for chainIdx := startIndex; chainIdx < len(modelChain); chainIdx++ {
		modelEntry := modelChain[chainIdx]
		maxAttempts := 1 + fallbackRetries

		for attempt := range maxAttempts {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			if attempt > 0 {
				backoffDelay := backoff.Calculate(attempt - 1)
				logRetryBackoff(a.Name(), modelEntry.provider.ID(), attempt, backoffDelay)
				if !backoff.SleepWithContext(ctx, backoffDelay) {
					return nil, ctx.Err()
				}
			}

			if chainIdx > startIndex && attempt == 0 {
				logFallbackAttempt(a.Name(), modelEntry, attempt, fallbackRetries, lastErr)
				prevModelID := modelChain[chainIdx-1].provider.ID()
				reason := ""
				if lastErr != nil {
					reason = lastErr.Error()
				}
				phase.Events <- ModelFallback(
					a.Name(),
					prevModelID,
					modelEntry.provider.ID(),
					reason,
					attempt+1,
					maxAttempts,
				)
			}

			slog.Debug("Executing model attempt",
				"agent", a.Name(),
				"model", modelEntry.provider.ID(),
				"is_fallback", modelEntry.isFallback,
				"in_cooldown", inCooldown,
				"attempt", attempt+1)

			attemptPhase := *phase
			attemptPhase.Model = modelEntry.provider
			attemptPhase.ModelDefinition = r.modelDefinitionForProvider(ctx, modelEntry.provider, phase.ModelDefinition)
			result, err := next(ctx, &attemptPhase)
			if err != nil {
				lastErr = err
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}

				decision := r.handleModelError(ctx, err, a, modelEntry, attempt, hasFallbacks, &primaryFailedWithNonRetryable)
				if decision == retryDecisionReturn {
					return nil, ctx.Err()
				} else if decision == retryDecisionBreak {
					break
				}
				continue
			}

			switch {
			case modelEntry.isFallback && primaryFailedWithNonRetryable:
				r.setCooldownState(a.Name(), modelEntry.index, getEffectiveCooldown(a))
			case !modelEntry.isFallback:
				r.clearCooldownState(a.Name())
			}

			if result == nil {
				result = &ModelResult{}
			}
			result.UsedModel = modelEntry.provider
			return result, nil
		}
	}

	if lastErr != nil {
		prefix := "model failed"
		if hasFallbacks {
			prefix = "all models failed"
		}
		wrapped := fmt.Errorf("%s: %w", prefix, lastErr)
		if modelerrors.IsContextOverflowError(lastErr) {
			return nil, modelerrors.NewContextOverflowError(wrapped)
		}
		return nil, wrapped
	}
	return nil, errors.New("model failed with unknown error")
}

func (r *LocalRuntime) modelDefinitionForProvider(ctx context.Context, model provider.Provider, fallback *modelsdev.Model) *modelsdev.Model {
	if model == nil {
		return fallback
	}
	if fallback != nil && fallback.Name == model.ID() {
		return fallback
	}
	if r.modelsStore == nil {
		return fallback
	}
	definition, err := r.modelsStore.GetModel(ctx, model.ID())
	if err != nil {
		slog.Debug("Failed to resolve model definition for provider", "model", model.ID(), "error", err)
		return fallback
	}
	return definition
}

// retryDecision is the outcome of handleModelError.
type retryDecision int

const (
	// retryDecisionContinue means retry the same model (backoff already applied).
	retryDecisionContinue retryDecision = iota
	// retryDecisionBreak means skip to the next model in the fallback chain.
	retryDecisionBreak
	// retryDecisionReturn means context was cancelled; return immediately.
	retryDecisionReturn
)

// handleModelError classifies err and decides what to do next:
//   - retryDecisionReturn   — context cancelled while sleeping; caller returns ctx.Err()
//   - retryDecisionBreak    — non-retryable error or 429 with fallbacks; skip to next model
//   - retryDecisionContinue — retryable error or 429 without fallbacks; retry same model
//
// Side-effect: sets *primaryFailedWithNonRetryable when the primary model fails with a
// non-retryable (or rate-limited-with-fallbacks) error.
func (r *LocalRuntime) handleModelError(
	ctx context.Context,
	err error,
	a *agent.Agent,
	modelEntry modelWithFallback,
	attempt int,
	hasFallbacks bool,
	primaryFailedWithNonRetryable *bool,
) retryDecision {
	retryable, rateLimited, retryAfter := modelerrors.ClassifyModelError(err)

	if rateLimited {
		// Gate: only retry on 429 if opt-in is enabled AND no fallbacks exist.
		// Default behavior (retryOnRateLimit=false) treats 429 as non-retryable,
		// identical to today's behavior before this feature was added.
		if !r.retryOnRateLimit || hasFallbacks {
			slog.Warn("Rate limited, treating as non-retryable",
				"agent", a.Name(),
				"model", modelEntry.provider.ID(),
				"retry_on_rate_limit_enabled", r.retryOnRateLimit,
				"has_fallbacks", hasFallbacks,
				"error", err)
			if !modelEntry.isFallback {
				*primaryFailedWithNonRetryable = true
			}
			return retryDecisionBreak
		}

		// Opt-in enabled, no fallbacks → retry same model after honouring Retry-After (or backoff).
		waitDuration := retryAfter
		if waitDuration <= 0 {
			waitDuration = backoff.Calculate(attempt)
		} else if waitDuration > backoff.MaxRetryAfterWait {
			slog.Warn("Retry-After exceeds maximum, capping",
				"agent", a.Name(),
				"model", modelEntry.provider.ID(),
				"retry_after", retryAfter,
				"max", backoff.MaxRetryAfterWait)
			waitDuration = backoff.MaxRetryAfterWait
		}
		slog.Warn("Rate limited, retrying (opt-in enabled)",
			"agent", a.Name(),
			"model", modelEntry.provider.ID(),
			"attempt", attempt+1,
			"wait", waitDuration,
			"retry_after_from_header", retryAfter > 0,
			"error", err)
		if !backoff.SleepWithContext(ctx, waitDuration) {
			return retryDecisionReturn
		}
		return retryDecisionContinue
	}

	if !retryable {
		slog.Error("Non-retryable error from model",
			"agent", a.Name(),
			"model", modelEntry.provider.ID(),
			"error", err)
		if !modelEntry.isFallback {
			*primaryFailedWithNonRetryable = true
		}
		return retryDecisionBreak
	}

	slog.Warn("Retryable error from model",
		"agent", a.Name(),
		"model", modelEntry.provider.ID(),
		"attempt", attempt+1,
		"error", err)
	return retryDecisionContinue
}

package runtime

import (
	"context"
	"errors"
	"log/slog"

	"github.com/docker/docker-agent/pkg/compaction"
	"github.com/docker/docker-agent/pkg/modelerrors"
)

func (r *LocalRuntime) installBuiltins() {
	r.modelMiddlewares = append(r.modelMiddlewares, r.compactionModelMiddleware())
	mergeLifecycleHooks(&r.lifecycleHooks, RuntimeLifecycleHooks{
		AfterToolBatch: []ToolBatchHook{r.compactionAfterToolBatchHook},
	})
}

func (r *LocalRuntime) compactionModelMiddleware() ModelMiddleware {
	return func(ctx context.Context, phase *ModelPhase, next ModelHandler) (*ModelResult, error) {
		if phase == nil {
			return next(ctx, phase)
		}

		if phase.ModelDefinition != nil {
			phase.Turn.ContextLimit = int64(phase.ModelDefinition.Limit.Context)
			if r.sessionCompaction && compaction.ShouldCompact(phase.Session.InputTokens, phase.Session.OutputTokens, 0, phase.Turn.ContextLimit) {
				r.Summarize(ctx, phase.Session, "", phase.Events)
				if err := rebuildModelPhasePromptContext(ctx, phase); err != nil {
					return nil, err
				}
			}
		}

		result, err := next(ctx, phase)
		if err == nil {
			phase.Execution.overflowCompactions = 0
			return result, nil
		}

		if _, ok := errors.AsType[*modelerrors.ContextOverflowError](err); ok &&
			r.sessionCompaction &&
			phase.Execution.overflowCompactions < maxOverflowCompactions {
			phase.Execution.overflowCompactions++
			slog.Warn("Context window overflow detected, attempting auto-compaction",
				"agent", phase.Agent.Name(),
				"session_id", phase.Session.ID,
				"input_tokens", phase.Session.InputTokens,
				"output_tokens", phase.Session.OutputTokens,
				"context_limit", phase.Turn.ContextLimit,
				"attempt", phase.Execution.overflowCompactions,
			)
			phase.Events <- Warning(
				"The conversation has exceeded the model's context window. Automatically compacting the conversation history...",
				phase.Agent.Name(),
			)
			r.Summarize(ctx, phase.Session, "", phase.Events)
			if err := rebuildModelPhasePromptContext(ctx, phase); err != nil {
				return nil, err
			}

			result, retryErr := next(ctx, phase)
			if retryErr == nil {
				phase.Execution.overflowCompactions = 0
			}
			return result, retryErr
		}

		return nil, err
	}
}

func rebuildModelPhasePromptContext(ctx context.Context, phase *ModelPhase) error {
	if phase == nil || phase.Session == nil || phase.Agent == nil {
		return nil
	}

	buildPhase := &BuildContextPhase{
		Runtime:         phase.Runtime,
		Execution:       phase.Execution,
		Session:         phase.Session,
		Agent:           phase.Agent,
		Turn:            phase.Turn,
		Model:           phase.Model,
		ModelDefinition: phase.ModelDefinition,
		PromptContext:   phase.PromptContext,
	}
	if err := phase.Runtime.buildContext(ctx, buildPhase); err != nil {
		return err
	}

	phase.PromptContext = buildPhase.PromptContext
	phase.Messages = buildPhase.PromptContext.Messages
	if phase.Turn != nil {
		phase.Turn.PromptContext = buildPhase.PromptContext
		phase.Turn.PromptMessages = buildPhase.PromptContext.Messages
	}
	return nil
}

func (r *LocalRuntime) compactionAfterToolBatchHook(ctx context.Context, phase *ToolBatchPhase) error {
	if phase == nil || phase.Turn == nil {
		return nil
	}
	r.compactIfNeeded(ctx, phase.Session, phase.Agent, phase.Turn.ModelDefinition, phase.Turn.ContextLimit, phase.Turn.MessageCountBeforeTools, phase.Events)
	return nil
}

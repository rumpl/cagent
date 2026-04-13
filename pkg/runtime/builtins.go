package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/compaction"
	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/modelerrors"
	"github.com/docker/docker-agent/pkg/session"
)

func (r *LocalRuntime) installBuiltins() {
	r.buildContextMiddlewares = append(r.buildContextMiddlewares, r.promptInjectionBuildContextMiddleware())
	r.modelMiddlewares = append(r.modelMiddlewares,
		r.compactionModelMiddleware(),
		r.fallbackModelMiddleware(),
	)
	r.toolMiddlewares = append(r.toolMiddlewares,
		r.approvalToolMiddleware(),
		r.shellHookToolMiddleware(),
	)
	mergeRuntimeObservers(&r.observers, RuntimeObservers{
		Notifications: []NotificationObserver{r.notificationHookObserver},
	})
	mergeLifecycleHooks(&r.lifecycleHooks, RuntimeLifecycleHooks{
		SessionStart:       []SessionHook{r.sessionStartHooksLifecycleAdapter},
		SessionEnd:         []SessionHook{r.sessionEndHooksLifecycleAdapter},
		BeforePauseForUser: []PauseHook{r.onUserInputPauseHookAdapter},
		TurnEnd:            []TurnHook{r.stopHooksTurnAdapter},
		AfterToolBatch:     []ToolBatchHook{r.compactionAfterToolBatchHook},
	})
}

func (r *LocalRuntime) promptInjectionBuildContextMiddleware() BuildContextMiddleware {
	return func(ctx context.Context, phase *BuildContextPhase, next BuildContextHandler) error {
		if err := next(ctx, phase); err != nil {
			return err
		}
		if phase == nil || phase.PromptContext == nil || phase.Agent == nil || phase.Session == nil {
			return nil
		}

		contextMessages := buildRuntimeContextSystemMessages(phase.Agent, phase.Session, phase.Execution)
		if len(contextMessages) == 0 {
			return nil
		}
		contextMessages[len(contextMessages)-1].CacheControl = true
		phase.PromptContext.InsertContextSystemMessages(contextMessages...)
		return nil
	}
}

func buildRuntimeContextSystemMessages(a *agent.Agent, sess *session.Session, exec *Execution) []chat.Message {
	messages := append([]chat.Message{}, session.BuildContextSpecificSystemMessages(a, sess)...)
	if exec != nil && len(exec.sessionPromptMessages) > 0 {
		messages = append(messages, exec.sessionPromptMessages...)
	}
	return messages
}

func appendExecutionPromptContext(exec *Execution, additionalContext string) {
	if exec == nil || strings.TrimSpace(additionalContext) == "" {
		return
	}
	msg := chat.Message{
		Role:      chat.MessageRoleSystem,
		Content:   additionalContext,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	exec.sessionPromptMessages = append(exec.sessionPromptMessages, msg)
}

func (r *LocalRuntime) notificationHookObserver(ctx context.Context, observed *ObservedNotification) error {
	if observed == nil || observed.Agent == nil {
		return nil
	}
	if observed.Level != "error" && observed.Level != "warning" {
		slog.Error("Invalid notification level", "level", observed.Level, "expected", "error|warning")
		return nil
	}

	hooksExec := r.getHooksExecutor(observed.Agent)
	if hooksExec == nil || !hooksExec.HasNotificationHooks() {
		return nil
	}

	slog.Debug("Executing notification hooks", "level", observed.Level, "session_id", observed.SessionID)
	_, err := hooksExec.ExecuteNotification(ctx, &hooks.Input{
		SessionID:           observed.SessionID,
		Cwd:                 r.workingDir,
		NotificationLevel:   observed.Level,
		NotificationMessage: observed.Message,
	})
	if err != nil {
		slog.Warn("Notification hook execution failed", "error", err)
	}
	return nil
}

func (r *LocalRuntime) sessionStartHooksLifecycleAdapter(ctx context.Context, phase *SessionPhase) error {
	if phase == nil || phase.Agent == nil || phase.Session == nil {
		return nil
	}

	hooksExec := r.getHooksExecutor(phase.Agent)
	if hooksExec == nil || !hooksExec.HasSessionStartHooks() {
		return nil
	}

	slog.Debug("Executing session start hooks", "agent", phase.Agent.Name(), "session_id", phase.Session.ID)
	result, err := hooksExec.ExecuteSessionStart(ctx, &hooks.Input{
		SessionID: phase.Session.ID,
		Cwd:       r.workingDir,
		Source:    "startup",
	})
	if err != nil {
		slog.Warn("Session start hook execution failed", "agent", phase.Agent.Name(), "error", err)
		return nil
	}
	if result.SystemMessage != "" {
		r.emitWarning(ctx, phase.Events, phase.Agent, phase.Session.ID, result.SystemMessage)
	}
	appendExecutionPromptContext(phase.Execution, result.AdditionalContext)
	return nil
}

func (r *LocalRuntime) sessionEndHooksLifecycleAdapter(ctx context.Context, phase *SessionPhase) error {
	if phase == nil || phase.Agent == nil || phase.Session == nil {
		return nil
	}

	hooksExec := r.getHooksExecutor(phase.Agent)
	if hooksExec == nil || !hooksExec.HasSessionEndHooks() {
		return nil
	}

	slog.Debug("Executing session end hooks", "agent", phase.Agent.Name(), "session_id", phase.Session.ID)
	_, err := hooksExec.ExecuteSessionEnd(ctx, &hooks.Input{
		SessionID: phase.Session.ID,
		Cwd:       r.workingDir,
		Reason:    "stream_ended",
	})
	if err != nil {
		slog.Error("Session end hook execution failed", "agent", phase.Agent.Name(), "error", err)
	}
	return nil
}

func (r *LocalRuntime) onUserInputPauseHookAdapter(ctx context.Context, phase *PausePhase) error {
	if phase == nil || phase.Agent == nil || phase.Session == nil {
		return nil
	}

	hooksExec := r.getHooksExecutor(phase.Agent)
	if hooksExec == nil || !hooksExec.HasOnUserInputHooks() {
		return nil
	}

	slog.Debug("Executing on-user-input hooks", "reason", phase.Reason, "session_id", phase.Session.ID)
	_, err := hooksExec.ExecuteOnUserInput(ctx, &hooks.Input{
		SessionID: phase.Session.ID,
		Cwd:       r.workingDir,
	})
	if err != nil {
		slog.Warn("On-user-input hook execution failed", "error", err)
	}
	return nil
}

func (r *LocalRuntime) stopHooksTurnAdapter(ctx context.Context, phase *TurnPhase) error {
	if phase == nil || phase.Turn == nil || phase.Agent == nil || phase.Session == nil || !phase.Turn.Result.Stopped {
		return nil
	}

	hooksExec := r.getHooksExecutor(phase.Agent)
	if hooksExec == nil || !hooksExec.HasStopHooks() {
		return nil
	}

	slog.Debug("Executing stop hooks", "agent", phase.Agent.Name(), "session_id", phase.Session.ID)
	result, err := hooksExec.ExecuteStop(ctx, &hooks.Input{
		SessionID:    phase.Session.ID,
		Cwd:          r.workingDir,
		StopResponse: phase.Turn.Result.Content,
	})
	if err != nil {
		slog.Warn("Stop hook execution failed", "agent", phase.Agent.Name(), "error", err)
		return nil
	}
	if result.SystemMessage != "" {
		r.emitWarning(ctx, phase.Events, phase.Agent, phase.Session.ID, result.SystemMessage)
	}
	appendExecutionPromptContext(phase.Execution, result.AdditionalContext)
	return nil
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
			r.emitWarning(ctx, phase.Events, phase.Agent, phase.Session.ID, "The conversation has exceeded the model's context window. Automatically compacting the conversation history...")
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

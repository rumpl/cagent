package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/compaction"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelerrors"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/telemetry"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin"
	bgagent "github.com/docker/docker-agent/pkg/tools/builtin/agent"
)

const maxOverflowCompactions = 1

// Execution holds the mutable state for a single RunStream invocation.
// It owns the control channels, queued inputs, iteration counters, and
// the currently active agent for the run.
type Execution struct {
	runtime *LocalRuntime
	session *session.Session
	events  chan Event

	activeAgent *agent.Agent

	iteration            int
	runtimeMaxIterations int
	overflowCompactions  int
	toolModelOverride    string
	prevAgentName        string
	loopDetector         *toolLoopDetector

	resumeChan            chan ResumeRequest
	elicitationRequestCh  chan ElicitationResult
	steerQueue            MessageQueue
	followUpQueue         MessageQueue
	sessionScratch        map[string]any
	turn                  *Turn
	pauseMu               sync.RWMutex
	waitingForResume      bool
	waitingForElicitation bool
}

// Turn holds the mutable state for a single loop iteration.
type Turn struct {
	Number                  int
	Agent                   *agent.Agent
	Tools                   []tools.Tool
	Model                   provider.Provider
	ModelID                 string
	ModelDefinition         *modelsdev.Model
	ContextLimit            int64
	PromptContext           *session.PromptContext
	PromptMessages          []chat.Message
	MessageCountBeforeTools int
	Result                  streamResult
}

// userInputAction normalizes the different ways user input enters a running
// execution: initial externally-appended input, steer injections, and follow-up
// messages that start a fresh turn.
type userInputAction struct {
	DisplayContent string
	StoredContent  string
	MultiContent   []chat.MessagePart
	Append         bool
	SessionPos     int
}

func newUserInputAction(displayContent, storedContent string, multiContent []chat.MessagePart) userInputAction {
	return userInputAction{
		DisplayContent: displayContent,
		StoredContent:  storedContent,
		MultiContent:   multiContent,
		Append:         true,
		SessionPos:     -1,
	}
}

func (r *LocalRuntime) newExecution(sess *session.Session, events chan Event) *Execution {
	loopThreshold := sess.MaxConsecutiveToolCalls
	if loopThreshold == 0 {
		loopThreshold = 5
	}

	exec := &Execution{
		runtime:              r,
		session:              sess,
		events:               events,
		activeAgent:          r.resolveSessionAgent(sess),
		runtimeMaxIterations: sess.MaxIterations,
		loopDetector: newToolLoopDetector(loopThreshold,
			bgagent.ToolNameViewBackgroundAgent,
			builtin.ToolNameViewBackgroundJob,
		),
		resumeChan:           make(chan ResumeRequest),
		elicitationRequestCh: make(chan ElicitationResult),
		steerQueue:           r.newSteerQueue(),
		followUpQueue:        r.newFollowUpQueue(),
		sessionScratch:       make(map[string]any),
	}

	for _, msg := range r.pendingSteerQueue.Drain(context.Background()) {
		exec.steerQueue.Enqueue(context.Background(), msg)
	}
	for _, msg := range r.pendingFollowUpQueue.Drain(context.Background()) {
		exec.followUpQueue.Enqueue(context.Background(), msg)
	}

	return exec
}

func (r *LocalRuntime) newSteerQueue() MessageQueue {
	if r.steerQueueFactory != nil {
		if q := r.steerQueueFactory(); q != nil {
			return q
		}
	}
	return NewInMemoryMessageQueue(defaultSteerQueueCapacity)
}

func (r *LocalRuntime) newFollowUpQueue() MessageQueue {
	if r.followUpQueueFactory != nil {
		if q := r.followUpQueueFactory(); q != nil {
			return q
		}
	}
	return NewInMemoryMessageQueue(defaultFollowUpQueueCapacity)
}

func (r *LocalRuntime) registerExecution(exec *Execution) {
	r.executionsMu.Lock()
	defer r.executionsMu.Unlock()
	r.executions = append(r.executions, exec)
}

func (r *LocalRuntime) unregisterExecution(exec *Execution) {
	r.executionsMu.Lock()
	defer r.executionsMu.Unlock()
	for i := len(r.executions) - 1; i >= 0; i-- {
		if r.executions[i] == exec {
			r.executions = append(r.executions[:i], r.executions[i+1:]...)
			return
		}
	}
}

func (r *LocalRuntime) currentExecution() *Execution {
	r.executionsMu.RLock()
	defer r.executionsMu.RUnlock()
	if len(r.executions) == 0 {
		return nil
	}
	return r.executions[len(r.executions)-1]
}

func (r *LocalRuntime) executionForSession(sess *session.Session) *Execution {
	if sess == nil {
		return nil
	}

	r.executionsMu.RLock()
	defer r.executionsMu.RUnlock()
	for i := len(r.executions) - 1; i >= 0; i-- {
		exec := r.executions[i]
		if exec == nil || exec.session == nil {
			continue
		}
		if exec.session == sess || exec.session.ID == sess.ID {
			return exec
		}
	}
	return nil
}

func (r *LocalRuntime) latestExecutionMatching(match func(*Execution) bool) *Execution {
	r.executionsMu.RLock()
	defer r.executionsMu.RUnlock()
	for i := len(r.executions) - 1; i >= 0; i-- {
		exec := r.executions[i]
		if exec != nil && match(exec) {
			return exec
		}
	}
	return nil
}

func (r *LocalRuntime) resumeTargetExecution() *Execution {
	if exec := r.latestExecutionMatching(func(exec *Execution) bool { return exec.isWaitingForResume() }); exec != nil {
		return exec
	}
	return r.currentExecution()
}

func (r *LocalRuntime) elicitationTargetExecution() *Execution {
	if exec := r.latestExecutionMatching(func(exec *Execution) bool { return exec.isWaitingForElicitation() }); exec != nil {
		return exec
	}
	return r.currentExecution()
}

func (e *Execution) resolveAgent() *agent.Agent {
	a := e.runtime.resolveSessionAgent(e.session)
	e.activeAgent = a
	return a
}

func (e *Execution) currentAgentName() string {
	if e.activeAgent != nil {
		return e.activeAgent.Name()
	}
	if a := e.resolveAgent(); a != nil {
		return a.Name()
	}
	return ""
}

func (e *Execution) currentSessionID() string {
	if e.session == nil {
		return ""
	}
	return e.session.ID
}

func (e *Execution) setWaitingForResume(waiting bool) {
	e.pauseMu.Lock()
	defer e.pauseMu.Unlock()
	e.waitingForResume = waiting
}

func (e *Execution) isWaitingForResume() bool {
	e.pauseMu.RLock()
	defer e.pauseMu.RUnlock()
	return e.waitingForResume
}

func (e *Execution) setWaitingForElicitation(waiting bool) {
	e.pauseMu.Lock()
	defer e.pauseMu.Unlock()
	e.waitingForElicitation = waiting
}

func (e *Execution) isWaitingForElicitation() bool {
	e.pauseMu.RLock()
	defer e.pauseMu.RUnlock()
	return e.waitingForElicitation
}

func (e *Execution) waitForResume(ctx context.Context) (ResumeRequest, bool) {
	e.setWaitingForResume(true)
	defer e.setWaitingForResume(false)

	select {
	case req := <-e.resumeChan:
		return req, true
	case <-ctx.Done():
		return ResumeRequest{}, false
	}
}

func (e *Execution) waitForElicitation(ctx context.Context) (ElicitationResult, error) {
	e.setWaitingForElicitation(true)
	defer e.setWaitingForElicitation(false)

	select {
	case result := <-e.elicitationRequestCh:
		return result, nil
	case <-ctx.Done():
		return ElicitationResult{}, ctx.Err()
	}
}

func (e *Execution) observeInitialUserInput(ctx context.Context) error {
	if !e.session.SendUserMessage {
		return nil
	}

	msg, pos, ok := lastUserMessageInSession(e.session)
	if !ok {
		return nil
	}

	return e.applyUserInput(ctx, userInputAction{
		DisplayContent: msg.Message.Content,
		StoredContent:  msg.Message.Content,
		MultiContent:   msg.Message.MultiContent,
		Append:         false,
		SessionPos:     pos,
	})
}

func (e *Execution) appendSteerInput(ctx context.Context, msg QueuedMessage) error {
	wrapped := fmt.Sprintf(
		"<system-reminder>\nThe user sent the following message while you were working:\n%s\n\nPlease address this in your next response while continuing with your current tasks.\n</system-reminder>",
		msg.Content,
	)
	return e.applyUserInput(ctx, newUserInputAction(msg.Content, wrapped, msg.MultiContent))
}

func (e *Execution) appendFollowUpInput(ctx context.Context, msg QueuedMessage) error {
	return e.applyUserInput(ctx, newUserInputAction(msg.Content, msg.Content, msg.MultiContent))
}

func (e *Execution) applyUserInput(ctx context.Context, input userInputAction) error {
	a := e.resolveAgent()
	phase := &UserMessagePhase{
		Runtime:      e.runtime,
		Execution:    e,
		Session:      e.session,
		Agent:        a,
		Events:       e.events,
		Input:        &input,
		DisplayInput: input.DisplayContent,
	}
	if err := e.runtime.runUserMessageHooks(ctx, e.runtime.lifecycleHooks.BeforeUserMessage, phase); err != nil {
		return err
	}

	pos := phase.Input.SessionPos
	if phase.Input.Append {
		userMsg := session.UserMessage(phase.Input.StoredContent, phase.Input.MultiContent...)
		e.session.AddMessage(userMsg)
		pos = len(e.session.Messages) - 1
	}
	e.events <- UserMessage(phase.Input.DisplayContent, e.currentSessionID(), phase.Input.MultiContent, pos)

	phase.Input.SessionPos = pos
	return e.runtime.runUserMessageHooks(ctx, e.runtime.lifecycleHooks.AfterUserMessage, phase)
}

func lastUserMessageInSession(sess *session.Session) (*session.Message, int, bool) {
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		item := sess.Messages[i]
		if !item.IsMessage() {
			continue
		}
		if item.Message.Message.Role != chat.MessageRoleUser {
			continue
		}
		return item.Message, i, true
	}
	return nil, -1, false
}

func (e *Execution) finalize(ctx context.Context) {
	defer close(e.events)

	a := e.resolveAgent()
	if a != nil {
		phase := &SessionPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Events: e.events}
		if err := e.runtime.runSessionHooks(context.WithoutCancel(ctx), e.runtime.lifecycleHooks.SessionEnd, phase); err != nil {
			slog.Warn("Session end lifecycle hook failed", "agent", a.Name(), "error", err)
		}
		e.runtime.executeSessionEndHooks(context.WithoutCancel(ctx), e.session, a)
		e.events <- StreamStopped(e.currentSessionID(), a.Name())
	} else {
		e.events <- StreamStopped(e.currentSessionID(), "")
	}

	e.runtime.executeOnUserInputHooks(ctx, e.currentSessionID(), "stream stopped")
	telemetry.RecordSessionEnd(ctx)
}

func (e *Execution) run(ctx context.Context) {
	a := e.resolveAgent()
	slog.Debug("Starting runtime stream", "agent", a.Name(), "session_id", e.currentSessionID())
	telemetry.RecordSessionStart(ctx, a.Name(), e.currentSessionID())

	ctx, sessionSpan := e.runtime.startSpan(ctx, "runtime.session", trace.WithAttributes(
		attribute.String("agent", a.Name()),
		attribute.String("session.id", e.currentSessionID()),
	))
	defer sessionSpan.End()
	defer e.finalize(ctx)

	if err := e.runtime.runSessionHooks(ctx, e.runtime.lifecycleHooks.SessionStart, &SessionPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Events: e.events}); err != nil {
		e.events <- Error(err.Error())
		return
	}
	e.runtime.executeSessionStartHooks(ctx, e.session, a, e.events)
	e.events <- TeamInfo(e.runtime.agentDetailsFromTeam(), a.Name())

	e.runtime.emitAgentWarnings(a, chanSend(e.events))
	e.runtime.configureToolsetHandlers(a, e.events)

	agentTools, err := e.runtime.getTools(ctx, a, sessionSpan, e.events)
	if err != nil {
		e.events <- Error(fmt.Sprintf("failed to get tools: %v", err))
		return
	}
	agentTools = filterExcludedTools(agentTools, e.session.ExcludedTools)
	e.events <- ToolsetInfo(len(agentTools), false, a.Name())
	if err := e.observeInitialUserInput(ctx); err != nil {
		e.events <- Error(err.Error())
		return
	}
	e.events <- StreamStarted(e.currentSessionID(), a.Name())

	for {
		a = e.resolveAgent()
		if a.Name() != e.prevAgentName {
			e.toolModelOverride = ""
			e.prevAgentName = a.Name()
		}

		e.runtime.emitAgentWarnings(a, chanSend(e.events))
		e.runtime.configureToolsetHandlers(a, e.events)

		agentTools, err = e.runtime.getTools(ctx, a, sessionSpan, e.events)
		if err != nil {
			e.events <- Error(fmt.Sprintf("failed to get tools: %v", err))
			return
		}
		agentTools = filterExcludedTools(agentTools, e.session.ExcludedTools)
		e.events <- ToolsetInfo(len(agentTools), false, a.Name())

		if e.runtimeMaxIterations > 0 && e.iteration >= e.runtimeMaxIterations {
			slog.Debug(
				"Maximum iterations reached",
				"agent", a.Name(),
				"iterations", e.iteration,
				"max", e.runtimeMaxIterations,
			)

			e.events <- MaxIterationsReached(e.runtimeMaxIterations)

			maxIterMsg := fmt.Sprintf("Maximum iterations reached (%d)", e.runtimeMaxIterations)
			e.runtime.executeNotificationHooks(ctx, a, e.currentSessionID(), "warning", maxIterMsg)
			e.runtime.executeOnUserInputHooks(ctx, e.currentSessionID(), "max iterations reached")

			if e.session.NonInteractive {
				slog.Debug("Auto-stopping after max iterations (non-interactive)", "agent", a.Name())

				assistantMessage := chat.Message{
					Role: chat.MessageRoleAssistant,
					Content: fmt.Sprintf(
						"Execution stopped after reaching the configured max_iterations limit (%d).",
						e.runtimeMaxIterations,
					),
					CreatedAt: time.Now().Format(time.RFC3339),
				}

				addAgentMessage(e.session, a, &assistantMessage, e.events)
				return
			}

			pausePhase := &PausePhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Events: e.events, Reason: "max_iterations"}
			if err := e.runtime.runPauseHooks(ctx, e.runtime.lifecycleHooks.BeforePauseForUser, pausePhase); err != nil {
				e.events <- Error(err.Error())
				return
			}

			req, ok := e.waitForResume(ctx)
			if !ok {
				slog.Debug(
					"Context cancelled while waiting for resume confirmation",
					"agent", a.Name(),
					"session_id", e.currentSessionID(),
				)
				return
			}

			if err := e.runtime.runResumeHooks(ctx, e.runtime.lifecycleHooks.AfterResume, &ResumePhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Events: e.events, Reason: "max_iterations", Request: req}); err != nil {
				e.events <- Error(err.Error())
				return
			}

			if req.Type == ResumeTypeApprove {
				slog.Debug("User chose to continue after max iterations", "agent", a.Name())
				e.runtimeMaxIterations = e.iteration + 10
			} else {
				slog.Debug("User rejected continuation", "agent", a.Name())

				assistantMessage := chat.Message{
					Role: chat.MessageRoleAssistant,
					Content: fmt.Sprintf(
						"Execution stopped after reaching the configured max_iterations limit (%d).",
						e.runtimeMaxIterations,
					),
					CreatedAt: time.Now().Format(time.RFC3339),
				}

				addAgentMessage(e.session, a, &assistantMessage, e.events)
				return
			}
		}

		e.iteration++
		if err := ctx.Err(); err != nil {
			slog.Debug("Runtime stream context cancelled, stopping loop", "agent", a.Name(), "session_id", e.currentSessionID())
			return
		}
		slog.Debug("Starting conversation loop iteration", "agent", a.Name(), "iteration", e.iteration)

		streamCtx, streamSpan := e.runtime.startSpan(ctx, "runtime.stream", trace.WithAttributes(
			attribute.String("agent", a.Name()),
			attribute.String("session.id", e.currentSessionID()),
		))

		turn := &Turn{Number: e.iteration, Agent: a, Tools: agentTools}
		e.turn = turn
		turnPhase := &TurnPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Turn: turn, Events: e.events}
		if err := e.runtime.runTurnHooks(ctx, e.runtime.lifecycleHooks.TurnStart, turnPhase); err != nil {
			e.events <- Error(err.Error())
			streamSpan.End()
			return
		}
		finishTurn := func() bool {
			if err := e.runtime.runTurnHooks(ctx, e.runtime.lifecycleHooks.TurnEnd, turnPhase); err != nil {
				e.events <- Error(err.Error())
				return false
			}
			return true
		}

		model := a.Model()
		if e.toolModelOverride != "" {
			if overrideModel, err := e.runtime.resolveModelRef(ctx, e.toolModelOverride); err != nil {
				slog.Warn("Failed to resolve per-tool model override; using agent default",
					"model_override", e.toolModelOverride, "error", err)
			} else {
				slog.Info("Using per-tool model override for this turn",
					"agent", a.Name(), "override", overrideModel.ID(), "primary", model.ID())
				model = overrideModel
			}
			e.toolModelOverride = ""
		}

		turn.Model = model
		turn.ModelID = model.ID()
		e.events <- AgentInfo(a.Name(), turn.ModelID, a.Description(), a.WelcomeMessage())

		slog.Debug("Using agent", "agent", a.Name(), "model", turn.ModelID)
		m, err := e.runtime.modelsStore.GetModel(ctx, turn.ModelID)
		if err != nil {
			slog.Debug("Failed to get model definition", "error", err)
		}
		turn.ModelDefinition = m

		if m != nil {
			turn.ContextLimit = int64(m.Limit.Context)
			if e.runtime.sessionCompaction && compaction.ShouldCompact(e.session.InputTokens, e.session.OutputTokens, 0, turn.ContextLimit) {
				e.runtime.Summarize(ctx, e.session, "", e.events)
			}
		}

		buildContextPhase := &BuildContextPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Turn: turn, Model: model, ModelDefinition: m}
		if err := e.runtime.buildContext(ctx, buildContextPhase); err != nil {
			e.events <- Error(err.Error())
			streamSpan.End()
			return
		}
		turn.PromptContext = buildContextPhase.PromptContext
		turn.PromptMessages = buildContextPhase.PromptContext.Messages

		modelResult, err := e.runtime.executeModelPhase(streamCtx, &ModelPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Turn: turn, PromptContext: turn.PromptContext, Messages: turn.PromptMessages, Tools: agentTools, Model: model, ModelDefinition: m, Events: e.events})
		var usedModel provider.Provider
		if modelResult != nil {
			usedModel = modelResult.UsedModel
		}
		var res streamResult
		if modelResult != nil {
			res = modelResult.Result
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				slog.Debug("Model stream canceled by context", "agent", a.Name(), "session_id", e.currentSessionID())
				streamSpan.End()
				return
			}

			if _, ok := errors.AsType[*modelerrors.ContextOverflowError](err); ok && e.runtime.sessionCompaction && e.overflowCompactions < maxOverflowCompactions {
				e.overflowCompactions++
				slog.Warn("Context window overflow detected, attempting auto-compaction",
					"agent", a.Name(),
					"session_id", e.currentSessionID(),
					"input_tokens", e.session.InputTokens,
					"output_tokens", e.session.OutputTokens,
					"context_limit", turn.ContextLimit,
					"attempt", e.overflowCompactions,
				)
				e.events <- Warning(
					"The conversation has exceeded the model's context window. Automatically compacting the conversation history...",
					a.Name(),
				)
				e.runtime.Summarize(ctx, e.session, "", e.events)
				streamSpan.End()
				continue
			}

			streamSpan.RecordError(err)
			streamSpan.SetStatus(codes.Error, "error handling stream")
			slog.Error("All models failed", "agent", a.Name(), "error", err)
			telemetry.RecordError(ctx, err.Error())
			errMsg := modelerrors.FormatError(err)
			e.events <- Error(errMsg)
			e.runtime.executeNotificationHooks(ctx, a, e.currentSessionID(), "error", errMsg)
			streamSpan.End()
			return
		}

		e.overflowCompactions = 0
		turn.Result = res

		if usedModel != nil && usedModel.ID() != model.ID() {
			slog.Info("Used fallback model", "agent", a.Name(), "primary", model.ID(), "used", usedModel.ID())
			e.events <- AgentInfo(a.Name(), usedModel.ID(), a.Description(), a.WelcomeMessage())
		}
		streamSpan.SetAttributes(
			attribute.Int("tool.calls", len(res.Calls)),
			attribute.Int("content.length", len(res.Content)),
			attribute.Bool("stopped", res.Stopped),
		)
		streamSpan.End()

		commitPhase := &AssistantCommitPhase{Runtime: e.runtime, Execution: e, Session: e.session, Agent: a, Turn: turn, Events: e.events, Result: res, AgentTools: agentTools, ModelID: turn.ModelID, ModelDefinition: m}
		if err := e.runtime.runAssistantCommitHooks(ctx, e.runtime.lifecycleHooks.BeforeAssistantCommit, commitPhase); err != nil {
			e.events <- Error(err.Error())
			return
		}
		msgUsage := e.runtime.recordAssistantMessage(e.session, a, res, agentTools, turn.ModelID, m, e.events)
		commitPhase.MessageUsage = msgUsage
		if err := e.runtime.runAssistantCommitHooks(ctx, e.runtime.lifecycleHooks.AfterAssistantCommit, commitPhase); err != nil {
			e.events <- Error(err.Error())
			return
		}
		usage := SessionUsage(e.session, turn.ContextLimit)
		usage.LastMessage = msgUsage
		e.events <- NewTokenUsageEvent(e.currentSessionID(), a.Name(), usage)

		turn.MessageCountBeforeTools = len(e.session.GetAllMessages())
		e.runtime.processToolCallsWithExecution(ctx, e, res.Calls, agentTools)

		if e.loopDetector.record(res.Calls) {
			toolName := "unknown"
			if len(res.Calls) > 0 {
				toolName = res.Calls[0].Function.Name
			}
			slog.Warn("Repetitive tool call loop detected",
				"agent", a.Name(), "tool", toolName,
				"consecutive", e.loopDetector.consecutive, "session_id", e.currentSessionID())
			errMsg := fmt.Sprintf(
				"Agent terminated: detected %d consecutive identical calls to %s. "+
					"This indicates a degenerate loop where the model is not making progress.",
				e.loopDetector.consecutive, toolName)
			e.events <- Error(errMsg)
			e.runtime.executeNotificationHooks(ctx, a, e.currentSessionID(), "error", errMsg)
			e.loopDetector.reset()
			return
		}

		e.toolModelOverride = resolveToolCallModelOverride(res.Calls, agentTools)

		if steered := e.steerQueue.Drain(ctx); len(steered) > 0 {
			for _, sm := range steered {
				if err := e.appendSteerInput(ctx, sm); err != nil {
					e.events <- Error(err.Error())
					return
				}
			}
			e.runtime.compactIfNeeded(ctx, e.session, a, m, turn.ContextLimit, turn.MessageCountBeforeTools, e.events)
			if !finishTurn() {
				return
			}
			continue
		}

		if res.Stopped {
			slog.Debug("Conversation stopped", "agent", a.Name())
			e.runtime.executeStopHooks(ctx, e.session, a, res.Content, e.events)

			if followUp, ok := e.followUpQueue.Dequeue(ctx); ok {
				if err := e.appendFollowUpInput(ctx, followUp); err != nil {
					e.events <- Error(err.Error())
					return
				}
				e.runtime.compactIfNeeded(ctx, e.session, a, m, turn.ContextLimit, turn.MessageCountBeforeTools, e.events)
				if !finishTurn() {
					return
				}
				continue
			}

			if !finishTurn() {
				return
			}
			break
		}

		e.runtime.compactIfNeeded(ctx, e.session, a, m, turn.ContextLimit, turn.MessageCountBeforeTools, e.events)
		if !finishTurn() {
			return
		}
	}
}

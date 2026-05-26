package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
	agenttool "github.com/docker/docker-agent/pkg/tools/builtin/agent"
	"github.com/docker/docker-agent/pkg/tools/builtin/handoff"
)

// agentNames returns the names of the given agents.
func agentNames(agents []*agent.Agent) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name()
	}
	return names
}

// validateAgentInList checks that targetAgent appears in the given agent list.
// Returns a tool error result if not found, or nil if the target is valid.
// The action describes the attempted operation (e.g. "transfer task to"),
// and listDesc is a human-readable description of the list (e.g. "sub-agents list").
func validateAgentInList(currentAgent, targetAgent, action, listDesc string, agents []*agent.Agent) *tools.ToolCallResult {
	if slices.ContainsFunc(agents, func(a *agent.Agent) bool { return a.Name() == targetAgent }) {
		return nil
	}
	if names := agentNames(agents); len(names) > 0 {
		return tools.ResultError(fmt.Sprintf(
			"Agent %s cannot %s %s: target agent not in %s. Available agent IDs are: %s",
			currentAgent, action, targetAgent, listDesc, strings.Join(names, ", "),
		))
	}
	return tools.ResultError(fmt.Sprintf(
		"Agent %s cannot %s %s: target agent not in %s. No agents are configured in this list.",
		currentAgent, action, targetAgent, listDesc,
	))
}

// buildTaskSystemMessage constructs the system message for a delegated task.
// attachedFiles, when non-empty, lists absolute paths of files the user
// attached to the parent conversation; they are surfaced to the sub-agent so
// it can use them directly without scanning the workspace or guessing from a
// bare filename.
//
// When sessionID is non-empty the sub-agent is a background agent; an
// identity block is appended so the agent knows its own session ID, who
// started it, and how to communicate back.
func buildTaskSystemMessage(task, expectedOutput string, attachedFiles []string, sessionID, parentAgentName string) string {
	var b strings.Builder
	b.WriteString("You are a member of a team of agents. Your goal is to complete the following task:")
	fmt.Fprintf(&b, "\n\n<task>\n%s\n</task>", task)
	if expectedOutput != "" {
		fmt.Fprintf(&b, "\n\n<expected_output>\n%s\n</expected_output>", expectedOutput)
	}
	if len(attachedFiles) > 0 {
		b.WriteString("\n\nThe user attached these files in the original conversation. They are available for you to read at these absolute paths; prefer them over any bare filenames mentioned in <task>:\n<attached_files>")
		for _, p := range attachedFiles {
			fmt.Fprintf(&b, "\n- %s", p)
		}
		b.WriteString("\n</attached_files>")
	}
	b.WriteString("\n\nIf the task references files, treat any absolute paths in <task> as authoritative and use them as-is. If a referenced file is given by name only (e.g. \"foo.go\"), do not guess: search the workspace or ask the calling agent for the absolute path before reading or modifying the file.")
	if sessionID != "" {
		b.WriteString("\n\nYou are running as a background agent.")
		fmt.Fprintf(&b, "\n- Your session ID is: %s", sessionID)
		if parentAgentName != "" {
			fmt.Fprintf(&b, "\n- You were started by agent: %s", parentAgentName)
		}
		b.WriteString("\n- Other background agents can address you by this session ID.")
		b.WriteString("\n- To send a message to another background agent, use send_message_background_agent with that agent's session_id.")
		b.WriteString("\n- To report back to the parent, write your response normally. The parent can read it via view_background_agent.")
	}
	return b.String()
}

// SubSessionConfig describes the shape of a child session: system prompt,
// implicit user message, agent identity, tool approval, exclusions, etc.
// It is the data input to [newSubSession]; the orchestration around running
// such a session (telemetry, current-agent switching, event forwarding)
// lives in [LocalRuntime.runForwarding] and [LocalRuntime.runCollecting].
type SubSessionConfig struct {
	// Task is the user-facing task description.
	Task string
	// ExpectedOutput is an optional description of what the sub-agent should produce.
	ExpectedOutput string
	// SystemMessage, when non-empty, replaces the default task-based system
	// message. This is used by skill sub-agents whose system prompt is the
	// skill content itself rather than the team delegation boilerplate.
	SystemMessage string
	// AgentName is the name of the agent that will execute the sub-session.
	AgentName string
	// Title is a human-readable label for the sub-session (e.g. "Transferred task").
	Title string
	// ToolsApproved overrides whether tools are pre-approved in the child session.
	ToolsApproved bool
	// NonInteractive marks the child session as running without a user present
	// (e.g. MCP server, A2A adapter, background agent). This causes the runtime
	// to auto-stop on max iterations instead of blocking for user input.
	NonInteractive bool
	// PinAgent, when true, pins the child session to AgentName via
	// session.WithAgentName. This is required for concurrent background
	// tasks that must not share the runtime's mutable currentAgent field.
	PinAgent bool
	// ImplicitUserMessage, when non-empty, overrides the default "Please proceed."
	// user message sent to the child session. This allows callers like skill
	// sub-agents to pass the task description as the user message.
	ImplicitUserMessage string
	// ExcludedTools lists tool names that should be filtered out of the agent's
	// tool list for the child session. This prevents recursive tool calls
	// (e.g. run_skill calling itself in a skill sub-session).
	ExcludedTools []string
	// SessionID, when non-empty, pre-assigns the child session's ID via
	// [session.WithID]. Background agent tasks set this so the parent can
	// refer to the task by session ID before the child session has been
	// constructed asynchronously. It is also injected into the child's
	// system prompt as the agent's own identity.
	SessionID string
	// ParentAgentName is the name of the agent that initiated this
	// sub-session. When SessionID is also set (background agent), it is
	// surfaced in the child's system prompt so the agent knows who started
	// it.
	ParentAgentName string
}

// delegationRequest bundles a [SubSessionConfig] with the single
// orchestration knob [LocalRuntime.runForwarding] needs: whether to
// swap the runtime's current agent for the lifetime of the call.
//
// Adding a new "spawn a sub-agent" feature is a matter of building one
// of these and calling runForwarding (or runCollecting for the
// non-interactive variant); the boilerplate around AgentInfo events,
// agent restoration, and event forwarding stays in runForwarding.
//
// The OpenTelemetry span is owned by the caller (each public-facing
// handler opens its own span before calling runForwarding) so that
// pre-delegation work — most importantly the model override applied
// by [LocalRuntime.handleRunSkill] before forwarding — is recorded
// under the caller's span.
type delegationRequest struct {
	SubSessionConfig

	// SwitchCurrentAgent, when true, swaps r.currentAgent to AgentName
	// for the lifetime of the call and emits AgentSwitching/AgentInfo
	// events on entry and exit. Used by transfer_task. Mutually
	// exclusive in spirit with PinAgent: pinning is for concurrent
	// sub-sessions that must NOT share the runtime's mutable
	// currentAgent, while switching is for sequential delegations where
	// the parent loop is blocked anyway.
	SwitchCurrentAgent bool
}

// newSubSession builds a *session.Session from a SubSessionConfig and a parent
// session. It consolidates the session options that were previously duplicated
// across handleTaskTransfer and RunAgent.
func newSubSession(parent *session.Session, cfg SubSessionConfig, childAgent *agent.Agent) *session.Session {
	// Sub-agents start in a fresh session, so they don't see the user's
	// original messages or attached files. Snapshot the parent's attached
	// files once and propagate them both to the system prompt (so the agent
	// is told about them) and to the child session (so further nested
	// transfers keep inheriting them).
	attachedFiles := parent.AttachedFilesSnapshot()

	sysMsg := cfg.SystemMessage
	if sysMsg == "" {
		sysMsg = buildTaskSystemMessage(cfg.Task, cfg.ExpectedOutput, attachedFiles, cfg.SessionID, cfg.ParentAgentName)
	}

	userMsg := cfg.ImplicitUserMessage
	if userMsg == "" {
		userMsg = "Please proceed."
	}

	opts := []session.Opt{
		session.WithSystemMessage(sysMsg),
		session.WithImplicitUserMessage(userMsg),
		session.WithMaxIterations(childAgent.MaxIterations()),
		session.WithMaxConsecutiveToolCalls(childAgent.MaxConsecutiveToolCalls()),
		session.WithMaxOldToolCallTokens(childAgent.MaxOldToolCallTokens()),
		session.WithTitle(cfg.Title),
		session.WithToolsApproved(cfg.ToolsApproved),
		session.WithNonInteractive(cfg.NonInteractive),
		session.WithSendUserMessage(false),
		session.WithParentID(parent.ID),
		session.WithAttachedFiles(attachedFiles),
	}
	if cfg.SessionID != "" {
		opts = append(opts, session.WithID(cfg.SessionID))
	}
	if cfg.PinAgent {
		opts = append(opts, session.WithAgentName(cfg.AgentName))
	}
	// Merge parent's excluded tools with config's excluded tools so that
	// nested sub-sessions (e.g. skill → transfer_task → child) inherit
	// exclusions from all ancestors and don't re-introduce filtered tools.
	excludedTools := mergeExcludedTools(parent.ExcludedTools, cfg.ExcludedTools)
	if len(excludedTools) > 0 {
		opts = append(opts, session.WithExcludedTools(excludedTools))
	}
	return session.New(opts...)
}

// mergeExcludedTools combines two excluded-tool lists, deduplicating entries.
// It returns nil when both inputs are empty.
func mergeExcludedTools(parent, child []string) []string {
	if len(parent) == 0 {
		return child
	}
	if len(child) == 0 {
		return parent
	}
	set := make(map[string]struct{}, len(parent)+len(child))
	for _, t := range parent {
		set[t] = struct{}{}
	}
	for _, t := range child {
		set[t] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for t := range set {
		merged = append(merged, t)
	}
	return merged
}

// swapCurrentAgent swaps the runtime's current agent from `from` to `to`,
// emitting the AgentSwitching/AgentInfo events and invoking the on_agent_switch
// hooks on entry, and returns a closure that reverses everything (restores
// `from`, emits the counterpart events and the matching return-side hooks)
// when invoked.
//
// Use as `defer r.swapCurrentAgent(ctx, sessionID, from, to, evts)()` so the
// swap takes effect immediately and the restore runs at function exit.
func (r *LocalRuntime) swapCurrentAgent(ctx context.Context, sessionID string, from, to *agent.Agent, evts EventSink) func() {
	evts.Emit(AgentSwitching(true, from.Name(), to.Name()))
	r.executeOnAgentSwitchHooks(ctx, from, sessionID, from.Name(), to.Name(), agentSwitchKindTransferTask)
	r.setCurrentAgent(to.Name())
	evts.Emit(AgentInfo(to.Name(), agentModelLabel(to), to.Description(), to.WelcomeMessage()))
	return func() {
		r.setCurrentAgent(from.Name())
		evts.Emit(AgentSwitching(false, to.Name(), from.Name()))
		r.executeOnAgentSwitchHooks(ctx, from, sessionID, to.Name(), from.Name(), agentSwitchKindTransferTaskReturn)
		evts.Emit(AgentInfo(from.Name(), agentModelLabel(from), from.Description(), from.WelcomeMessage()))
	}
}

// runForwarding runs a child session synchronously, forwarding all of its
// events to evts and propagating tool-approval state back to the parent
// on completion. This is the "interactive" path used by transfer_task and
// run_skill: the parent loop is blocked while the child executes, and
// the user sees the child's events live.
//
// On success it returns a tool result whose output is the child's last
// assistant message. On error it has already forwarded the ErrorEvent to
// evts and returns a wrapped error.
//
// The caller is expected to have opened a tracing span before calling
// runForwarding; the function records sub-session status (Ok / Error)
// on whatever span is attached to ctx — a no-op if none.
//
// runForwarding handles every concern the callers used to duplicate:
// swapping the current agent (if requested), resolving the child agent,
// building the sub-session, driving RunStream, and recording the
// sub-session on the parent.
func (r *LocalRuntime) runForwarding(ctx context.Context, parent *session.Session, evts EventSink, req delegationRequest) (*tools.ToolCallResult, error) {
	span := trace.SpanFromContext(ctx)

	callerAgent, err := r.team.Agent(r.CurrentAgentName())
	if err != nil {
		return nil, fmt.Errorf("current agent not found: %w", err)
	}
	child, err := r.team.Agent(req.AgentName)
	if err != nil {
		return nil, err
	}

	if req.SwitchCurrentAgent {
		defer r.swapCurrentAgent(ctx, parent.ID, callerAgent, child, evts)()
	}

	s := newSubSession(parent, req.SubSessionConfig, child)

	// subagent_stop fires after the child's stream has fully drained,
	// using the *parent* agent's executor so handlers configured on the
	// orchestrator see every child completion in one place — success or
	// failure. The deferred call ensures we don't lose the event when an
	// ErrorEvent triggers an early return below; handlers can detect a
	// failed run by an empty stop_response (or by correlating with the
	// session-level error event the parent already received).
	defer func() {
		r.executeSubagentStopHooks(ctx, parent, s, callerAgent, req.AgentName, s.GetLastAssistantMessageContent())
	}()

	childEvents := r.RunStream(ctx, s)
	for event := range childEvents {
		evts.Emit(event)
		if errEvent, ok := event.(*ErrorEvent); ok {
			// Drain remaining events (including StreamStoppedEvent) so the
			// TUI's streamDepth counter stays balanced.
			for remaining := range childEvents {
				evts.Emit(remaining)
			}
			err := fmt.Errorf("%s", errEvent.Error)
			span.RecordError(err)
			span.SetStatus(codes.Error, "sub-session error")
			return nil, err
		}
	}

	parent.ToolsApproved = s.ToolsApproved
	parent.AddSubSession(s)
	evts.Emit(SubSessionCompleted(parent.ID, s, callerAgent.Name()))

	span.SetStatus(codes.Ok, "sub-session completed")
	return tools.ResultSuccess(s.GetLastAssistantMessageContent()), nil
}

// runCollectingOnSession drives one RunStream invocation on s.
//
// eventSink, if non-nil, receives every event the runtime emits before
// the usual content/error handling. Background-agent tabs use this to
// forward events into the consumer App's event bus so the supervisor's
// existing tab routing displays the stream.
//
// linkToParent is true for the first invocation against a given child
// session (so the parent session records the sub-session) and false
// for every subsequent resume invocation.
func (r *LocalRuntime) runCollectingOnSession(ctx context.Context, parent, s *session.Session, cfg SubSessionConfig, onContent func(string), linkToParent bool, eventSink func(Event)) *agenttool.RunResult {
	// subagent_stop fires after the background sub-session has fully
	// drained — success or failure. The parent agent at the time of
	// dispatch (whoever called run_background_agent) owns the executor;
	// we resolve it via CurrentAgent because the background path doesn't
	// carry the parent agent name. dispatchHook silently no-ops when
	// CurrentAgent is nil. The deferred call ensures the hook fires even
	// when an ErrorEvent or ctx cancellation breaks us out of the loop.
	defer func() {
		r.executeSubagentStopHooks(ctx, parent, s, r.CurrentAgent(), cfg.AgentName, s.GetLastAssistantMessageContent())
	}()

	forward := func(event Event) {
		if eventSink != nil {
			eventSink(event)
		}
	}

	slog.DebugContext(ctx, "Background agent stream starting", "session_id", s.ID, "parent_session_id", parent.ID, "agent", cfg.AgentName, "link_to_parent", linkToParent, "max_iterations", s.MaxIterations, "non_interactive", s.NonInteractive)

	var errMsg string
	events := r.RunStream(ctx, s)
	for event := range events {
		forward(event)
		switch ev := event.(type) {
		case *StreamStoppedEvent:
			slog.DebugContext(ctx, "Background agent stream stopped event", "session_id", s.ID, "agent", cfg.AgentName, "reason", ev.Reason)
		case *MaxIterationsReachedEvent:
			slog.DebugContext(ctx, "Background agent reached max iterations", "session_id", s.ID, "agent", cfg.AgentName, "max_iterations", ev.MaxIterations)
		case *ErrorEvent:
			slog.DebugContext(ctx, "Background agent emitted error", "session_id", s.ID, "agent", cfg.AgentName, "error", ev.Error)
		}
		if ctx.Err() != nil {
			break
		}
		if choice, ok := event.(*AgentChoiceEvent); ok && choice.Content != "" {
			if onContent != nil {
				onContent(choice.Content)
			}
		}
		if errEvt, ok := event.(*ErrorEvent); ok {
			errMsg = errEvt.Error
			break
		}
	}
	// Drain remaining events so the RunStream goroutine can complete and
	// close the channel without blocking on a full buffer.
	for event := range events {
		forward(event)
	}

	if errMsg != "" {
		slog.DebugContext(ctx, "Background agent stream finished with error", "session_id", s.ID, "agent", cfg.AgentName, "error", errMsg)
		return &agenttool.RunResult{ErrMsg: errMsg}
	}

	result := s.GetLastAssistantMessageContent()
	if linkToParent {
		parent.AddSubSession(s)
	}

	slog.DebugContext(ctx, "Background agent stream finished", "session_id", s.ID, "agent", cfg.AgentName, "result_length", len(result), "message_count", len(s.Messages), "link_to_parent", linkToParent)
	return &agenttool.RunResult{Result: result}
}

// CurrentAgentSubAgentNames implements agenttool.Runner.
func (r *LocalRuntime) CurrentAgentSubAgentNames() []string {
	a := r.CurrentAgent()
	if a == nil {
		return nil
	}
	return agentNames(a.SubAgents())
}

// steerableRuntime adapts a *LocalRuntime to the agenttool.Steerable
// interface, translating the string content passed by
// send_message_background_agent into a [QueuedMessage]. Defined here
// (rather than in pkg/tools/builtin/agent) so the agent package can
// stay free of the QueuedMessage type and avoid an import cycle.
type steerableRuntime struct{ rt *LocalRuntime }

func (s steerableRuntime) Steer(content string) error {
	return s.rt.Steer(QueuedMessage{Content: content})
}

// RunAgent implements agenttool.Runner. It sets up the child runtime
// and session for a background task and returns immediately. Actual
// RunStream invocations are driven in response to signals on
// params.ResumeSignal:
//
//   - In TUI mode (a [LocalRuntime.OnBackgroundAgentStarted] handler is
//     registered) the consumer drives. RunAgent emits a
//     [BackgroundAgentStart] event that carries the child runtime and
//     session along with the RunBackground closure and the same
//     ResumeSignal channel; the consumer reads ResumeSignal and
//     invokes RunBackground from its own goroutine, typically wrapped
//     in an App so events flow through the TUI's tab routing.
//
//   - In headless mode RunAgent self-drives a goroutine that reads
//     ResumeSignal and invokes the same closure with a nil event sink.
//     The OnContent callback is the only output channel in this mode.
//
// Every invocation pushes its [agenttool.RunResult] on params.Completed
// so the Handler can update task status.
//
// Each background task gets its own child [LocalRuntime] so that
// send_message_background_agent can target one specific task's
// [LocalRuntime.steerQueue] instead of the parent's shared queue.
//
// Background tasks run with tools pre-approved because there is no user
// present to respond to interactive approval prompts during async
// execution.
func (r *LocalRuntime) RunAgent(ctx context.Context, params agenttool.RunParams) *agenttool.RunResult {
	if _, err := r.team.Agent(params.AgentName); err != nil {
		return &agenttool.RunResult{ErrMsg: fmt.Sprintf("agent %q not found: %s", params.AgentName, err)}
	}

	opts := []Opt{
		WithCurrentAgent(params.AgentName),
		WithWorkingDir(r.workingDir),
		WithEnv(r.env),
		WithNonInteractive(true),
		WithSessionStore(r.sessionStore),
		WithTracer(r.tracer),
		WithTelemetry(r.telemetry),
		WithClock(r.now),
		WithSessionCompaction(r.sessionCompaction),
		WithManagedOAuth(r.managedOAuth),
		WithMaxOverflowCompactions(r.maxOverflowCompactions),
		WithHooksRegistry(r.hooksRegistry),
		WithModelStore(r.modelsStore),
	}
	if r.modelSwitcherCfg != nil {
		opts = append(opts, WithModelSwitcherConfig(r.modelSwitcherCfg))
	}
	if r.fallback != nil && r.fallback.retryOnRateLimit {
		opts = append(opts, WithRetryOnRateLimit())
	}
	for _, obs := range r.observers {
		opts = append(opts, WithEventObserver(obs))
	}
	for _, t := range r.transforms {
		opts = append(opts, WithMessageTransform(t.name, t.fn))
	}
	for _, inj := range r.autoInjectors {
		opts = append(opts, WithAutoInjector(inj))
	}

	childRuntime, err := NewLocalRuntime(r.team, opts...)
	if err != nil {
		return &agenttool.RunResult{ErrMsg: fmt.Sprintf("failed to create child runtime: %s", err)}
	}
	childRuntime.bgAgents = agenttool.NewHandlerSharingTasks(childRuntime, r.bgAgents)
	childRuntime.registerDefaultTools()

	// Propagate the background-agent-started hook so the consumer (TUI)
	// is notified about grandchildren spawned from inside background
	// agents, not just direct children.
	if r.onBackgroundAgentStarted != nil {
		childRuntime.OnBackgroundAgentStarted(r.onBackgroundAgentStarted)
	}

	cfg := SubSessionConfig{
		Task:            params.Task,
		ExpectedOutput:  params.ExpectedOutput,
		AgentName:       params.AgentName,
		Title:           "Background agent task",
		ToolsApproved:   true,
		NonInteractive:  true,
		PinAgent:        true,
		SessionID:       params.SessionID,
		ParentAgentName: r.CurrentAgentName(),
	}

	child, err := r.team.Agent(params.AgentName)
	if err != nil {
		return &agenttool.RunResult{ErrMsg: fmt.Sprintf("agent %q not found: %s", params.AgentName, err)}
	}
	s := newSubSession(params.ParentSession, cfg, child)

	if params.OnRuntimeReady != nil {
		params.OnRuntimeReady(agenttool.TaskRuntime{
			Steerable: steerableRuntime{rt: childRuntime},
			SessionID: s.ID,
		})
	}

	// runOnce is the single driver primitive: one call drives one
	// RunStream invocation, forwarding events through eventSink, and
	// pushes the result on params.Completed. linkToParent flips after
	// the first call so resume invocations don't re-attach the
	// sub-session to the parent.
	var (
		linkedMu sync.Mutex
		linked   bool
	)
	runOnce := func(runCtx context.Context, eventSink func(Event)) {
		linkedMu.Lock()
		link := !linked
		linked = true
		linkedMu.Unlock()
		result := childRuntime.runCollectingOnSession(runCtx, params.ParentSession, s, cfg, params.OnContent, link, eventSink)
		if params.Completed != nil {
			select {
			case params.Completed <- result:
			case <-runCtx.Done():
			}
		}
	}

	if r.onBackgroundAgentStarted != nil {
		// TUI mode: hand off driving to the consumer. The consumer reads
		// ResumeSignal and invokes RunBackground from its own goroutine.
		r.emitBackgroundAgentStarted(BackgroundAgentStart{
			SessionID:     s.ID,
			AgentName:     params.AgentName,
			Runtime:       childRuntime,
			Session:       s,
			RunBackground: runOnce,
			ResumeSignal:  params.ResumeSignal,
		})
		return &agenttool.RunResult{}
	}

	// Headless mode: drive the loop ourselves. The goroutine lives
	// until ctx is canceled (HandleStop or runtime shutdown).
	go func() {
		for {
			select {
			case _, ok := <-params.ResumeSignal:
				if !ok {
					return
				}
				runOnce(ctx, nil)
			case <-ctx.Done():
				return
			}
		}
	}()
	return &agenttool.RunResult{}
}

func (r *LocalRuntime) handleTaskTransfer(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, evts EventSink) (*tools.ToolCallResult, error) {
	var params struct {
		Agent          string `json:"agent"`
		Task           string `json:"task"`
		ExpectedOutput string `json:"expected_output"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	a := r.CurrentAgent()
	if errResult := validateAgentInList(a.Name(), params.Agent, "transfer task to", "sub-agents list", a.SubAgents()); errResult != nil {
		return errResult, nil
	}

	slog.DebugContext(ctx, "Transferring task to agent", "from_agent", a.Name(), "to_agent", params.Agent, "task", params.Task)

	ctx, span := r.startSpan(ctx, "runtime.task_transfer", trace.WithAttributes(
		attribute.String("from.agent", a.Name()),
		attribute.String("to.agent", params.Agent),
		attribute.String("session.id", sess.ID),
	))
	defer span.End()

	return r.runForwarding(ctx, sess, evts, delegationRequest{
		SubSessionConfig: SubSessionConfig{
			Task:           params.Task,
			ExpectedOutput: params.ExpectedOutput,
			AgentName:      params.Agent,
			Title:          "Transferred task",
			ToolsApproved:  sess.ToolsApproved,
			NonInteractive: sess.NonInteractive,
		},
		SwitchCurrentAgent: true,
	})
}

func (r *LocalRuntime) handleHandoff(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, _ EventSink) (*tools.ToolCallResult, error) {
	var params handoff.Args
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ca := r.CurrentAgentName()
	currentAgent, err := r.team.Agent(ca)
	if err != nil {
		return nil, fmt.Errorf("current agent not found: %w", err)
	}

	if errResult := validateAgentInList(ca, params.Agent, "hand off to", "handoffs list", currentAgent.Handoffs()); errResult != nil {
		return errResult, nil
	}

	next, err := r.team.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	r.executeOnAgentSwitchHooks(ctx, currentAgent, sess.ID, ca, next.Name(), agentSwitchKindHandoff)
	r.setCurrentAgent(next.Name())
	handoffMessage := "The agent " + ca + " handed off the conversation to you. " +
		"Your available handoff agents and tools are specified in the system messages that follow. " +
		"Only use those capabilities - do not attempt to use tools or hand off to agents that you see " +
		"in the conversation history from previous agents, as those were available to different agents " +
		"with different capabilities. Look at the conversation history for context, but only use the " +
		"handoff agents and tools that are listed in your system messages below. " +
		"Complete your part of the task and hand off to the next appropriate agent in your workflow " +
		"(if any are available to you), or respond directly to the user if you are the final agent."
	return tools.ResultSuccess(handoffMessage), nil
}

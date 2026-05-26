package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/docker/docker-agent/pkg/concurrent"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

const (
	ToolNameRunBackgroundAgent         = "run_background_agent"
	ToolNameListBackgroundAgents       = "list_background_agents"
	ToolNameViewBackgroundAgent        = "view_background_agent"
	ToolNameStopBackgroundAgent        = "stop_background_agent"
	ToolNameSendMessageBackgroundAgent = "send_message_background_agent"
)

const (
	// maxConcurrentTasks is the maximum number of simultaneously running background agent tasks.
	maxConcurrentTasks = 20
	// maxTotalTasks caps total stored tasks (running + completed) to prevent unbounded memory growth.
	maxTotalTasks = 100
	// maxOutputBytes caps the live output buffer per task, mirroring the shell tool's limit.
	maxOutputBytes = 10 * 1024 * 1024 // 10 MB
)

// CreateToolSet is used by the tools registry.
func CreateToolSet() (tools.ToolSet, error) {
	return New(), nil
}

// RunBackgroundAgentArgs specifies the parameters for dispatching a sub-agent task asynchronously.
type RunBackgroundAgentArgs struct {
	Agent          string `json:"agent" jsonschema:"The name of the sub-agent to run in the background."`
	Task           string `json:"task" jsonschema:"A clear and concise description of the task the agent should achieve."`
	ExpectedOutput string `json:"expected_output,omitempty" jsonschema:"The expected output from the agent (optional)."`
}

// ViewBackgroundAgentArgs specifies the session ID to inspect.
type ViewBackgroundAgentArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID of the background agent to view."`
}

// StopBackgroundAgentArgs specifies the session ID to cancel.
type StopBackgroundAgentArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID of the background agent to stop."`
}

// SendMessageBackgroundAgentArgs specifies the parameters for sending a
// follow-up or steering message to a running background agent.
type SendMessageBackgroundAgentArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID of the background agent to send a message to."`
	Message   string `json:"message" jsonschema:"The message to send to the background agent."`
}

// Steerable is the minimal subset of a runtime that a background task
// needs in order to accept steering messages. Defined here (rather than
// imported from pkg/runtime) to avoid an import cycle: pkg/runtime
// already imports this package.
type Steerable interface {
	Steer(content string) error
}

// TaskRuntime bundles the per-task control surface exposed to the
// background agent handler. Steerable enqueues a message on the child
// runtime's steer queue; SessionID identifies the child session and is
// used as the public handle for the task (look-ups in [Handler.tasks],
// tool arguments). Populated by [Runner.RunAgent] via
// RunParams.OnRuntimeReady.
type TaskRuntime struct {
	Steerable Steerable
	SessionID string
}

// RunParams holds the parameters for running a sub-agent.
type RunParams struct {
	AgentName      string
	Task           string
	ExpectedOutput string
	ParentSession  *session.Session
	OnContent      func(content string)
	// SessionID, when non-empty, is the pre-assigned session ID for the
	// child sub-session. The runner forwards this to [session.WithID] so
	// the caller can use the session ID as a stable task handle from the
	// moment HandleRun returns, before the child session is constructed
	// asynchronously.
	SessionID string
	// OnRuntimeReady, when non-nil, is invoked by the runner with the
	// per-task control surface once the child runtime and sub-session
	// have been constructed but before any RunStream begins.
	OnRuntimeReady func(rt TaskRuntime)
	// ResumeSignal is read by [Runner.RunAgent] to determine when the
	// child session should run. The Handler sends on it once after
	// setup for the initial run, and again on every send_message that
	// resumes a completed task. In TUI mode the runtime forwards the
	// same channel to the consumer (via [BackgroundAgentStart]); the
	// consumer drains it and drives the run loop through its own
	// machinery. In headless mode the runtime drives the loop itself
	// in a goroutine that reads from this channel.
	ResumeSignal <-chan struct{}
	// Completed receives the [RunResult] of every RunStream invocation
	// the runtime performs on the child session. The Handler tracks
	// task status by reading from it.
	Completed chan<- *RunResult
}

// RunResult holds the outcome of a sub-agent execution.
type RunResult struct {
	Result string // final assistant message on completion
	ErrMsg string // error detail if failed
}

// Runner abstracts the runtime dependency for background agent execution.
type Runner interface {
	// CurrentAgentSubAgentNames returns the names of the current agent's sub-agents.
	CurrentAgentSubAgentNames() []string
	// RunAgent sets up a background task and returns immediately.
	//
	// Implementations create the child runtime and session, populate
	// the [TaskRuntime] handed back via [RunParams.OnRuntimeReady], and
	// arrange for RunStream invocations to happen each time the
	// caller sends on [RunParams.ResumeSignal]. Every invocation
	// pushes its outcome on [RunParams.Completed].
	//
	// The returned [RunResult] reports synchronous setup outcome only:
	// a non-empty ErrMsg means the task could not be set up and the
	// caller should NOT expect any signal on Completed.
	RunAgent(ctx context.Context, params RunParams) *RunResult
}

// taskStatus represents the lifecycle state of a background agent task.
type taskStatus int32

const (
	taskRunning taskStatus = iota
	taskCompleted
	taskStopped
	taskFailed
)

// String returns a human-readable name for the status.
func (s taskStatus) String() string {
	switch s {
	case taskRunning:
		return "running"
	case taskCompleted:
		return "completed"
	case taskStopped:
		return "stopped"
	case taskFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// task tracks a single background sub-agent execution. The task's public
// identity is its session ID, which is also the key in [Handler.tasks].
type task struct {
	sessionID string
	agentName string
	taskDesc  string

	cancel    context.CancelFunc
	startTime time.Time
	status    atomic.Int32
	result    string
	errMsg    string

	// runtime is the per-task Steerable, set once via
	// RunParams.OnRuntimeReady. nil until the runner has constructed
	// the child runtime; HandleSendMessage handles that race by
	// returning a transient error.
	runtime Steerable

	// needsRun, when sent on, tells the runtime (TUI consumer in TUI
	// mode, runtime's own goroutine in headless mode) to drive one
	// RunStream invocation on the child session. The Handler sends on
	// it for the initial run and on every send_message-driven resume.
	needsRun chan struct{}

	// ctx is the task-scoped context originally created by HandleRun.
	// Held so the result-tracking goroutine can detect stop/shutdown.
	ctx context.Context //nolint:containedctx // task lifecycle owns this context

	// outputMu protects output, outputBytes, viewCount, and lastViewedOutputBytes.
	outputMu              sync.RWMutex
	output                strings.Builder
	outputBytes           int
	viewCount             int
	lastViewedOutputBytes int
}

func (t *task) loadStatus() taskStatus {
	return taskStatus(t.status.Load())
}

func (t *task) storeStatus(s taskStatus) {
	t.status.Store(int32(s))
}

func (t *task) casStatus(old, next taskStatus) bool {
	return t.status.CompareAndSwap(int32(old), int32(next))
}

// writeOutput appends content to the task's live output buffer, respecting the
// maxOutputBytes cap. It is safe for concurrent use.
func (t *task) writeOutput(content string) {
	t.outputMu.Lock()
	defer t.outputMu.Unlock()

	if t.outputBytes < maxOutputBytes {
		n, _ := t.output.WriteString(content)
		t.outputBytes += n
	}
}

// formatView builds the human-readable output section for HandleView.
// It covers all terminal and in-progress states. The caller supplies the
// pre-loaded status and elapsed duration.
func (t *task) formatView(status taskStatus, elapsed time.Duration) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Session ID: %s\n", t.sessionID)
	fmt.Fprintf(&out, "Agent:      %s\n", t.agentName)
	fmt.Fprintf(&out, "Status:     %s\n", status)
	fmt.Fprintf(&out, "Runtime:    %s\n", elapsed)
	out.WriteString("\n--- Output ---\n")

	switch status {
	case taskCompleted:
		if t.result != "" {
			out.WriteString(t.result)
		} else {
			out.WriteString("<no output>")
		}

	case taskFailed:
		out.WriteString("<task failed>")
		if t.errMsg != "" {
			fmt.Fprintf(&out, "\nError: %s", t.errMsg)
		}

	case taskStopped:
		out.WriteString("<task was stopped>")

	default: // taskRunning (or any unexpected value)
		t.outputMu.Lock()
		progress := t.output.String()
		truncated := t.outputBytes >= maxOutputBytes
		currentBytes := t.outputBytes

		if currentBytes == t.lastViewedOutputBytes {
			t.viewCount++
		} else {
			t.viewCount = 1
			t.lastViewedOutputBytes = currentBytes
		}
		viewCount := t.viewCount
		t.outputMu.Unlock()

		if progress != "" {
			out.WriteString(progress)
			if truncated {
				out.WriteString("\n\n[output truncated at 10MB limit — still running...]")
			} else {
				out.WriteString("\n\n[still running...]")
			}
		} else {
			out.WriteString("<no output yet — still running>")
		}
		if viewCount > 1 {
			fmt.Fprintf(&out, "\n\n[No new output since last check — poll #%d]", viewCount)
		}
	}

	return out.String()
}

// Handler owns all background agent tasks and provides tool handlers.
type Handler struct {
	runner Runner
	wg     *sync.WaitGroup
	tasks  *concurrent.Map[string, *task]
}

// NewHandler creates a new Handler with the given Runner.
func NewHandler(runner Runner) *Handler {
	return &Handler{
		runner: runner,
		wg:     &sync.WaitGroup{},
		tasks:  concurrent.NewMap[string, *task](),
	}
}

// NewHandlerSharingTasks creates a Handler that uses runner for starting
// new background agents while sharing task lookup/control state with source.
// This lets independently-running background agent runtimes address the same
// session IDs, so siblings and descendants can send messages to each other.
func NewHandlerSharingTasks(runner Runner, source *Handler) *Handler {
	if source == nil {
		return NewHandler(runner)
	}
	return &Handler{
		runner: runner,
		wg:     source.wg,
		tasks:  source.tasks,
	}
}

// newSessionID returns a fresh UUID suitable for use as a background
// agent session ID. The same ID is forwarded to [session.WithID] inside
// the runtime, so the task handle the caller receives is the actual
// session ID of the child session.
func newSessionID() string {
	return uuid.New().String()
}

func (h *Handler) runningTaskCount() int {
	var count int
	h.tasks.Range(func(_ string, t *task) bool {
		if t.loadStatus() == taskRunning {
			count++
		}
		return true
	})
	return count
}

func (h *Handler) totalTaskCount() int {
	return h.tasks.Length()
}

func (h *Handler) pruneCompleted() {
	var toDelete []string
	h.tasks.Range(func(id string, t *task) bool {
		if s := t.loadStatus(); s != taskRunning {
			toDelete = append(toDelete, id)
		}
		return true
	})
	for _, id := range toDelete {
		h.tasks.Delete(id)
	}
}

// HandleRun sets up a sub-agent background task and returns its session
// ID immediately. The actual RunStream invocations are driven by the
// runtime in response to signals on the task's needsRun channel.
func (h *Handler) HandleRun(ctx context.Context, sess *session.Session, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params RunBackgroundAgentArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if strings.TrimSpace(params.Agent) == "" {
		return tools.ResultError("agent name must not be empty"), nil
	}
	if strings.TrimSpace(params.Task) == "" {
		return tools.ResultError("task must not be empty"), nil
	}

	subAgentNames := h.runner.CurrentAgentSubAgentNames()
	if !slices.Contains(subAgentNames, params.Agent) {
		if len(subAgentNames) > 0 {
			return tools.ResultError(fmt.Sprintf("agent %q is not in the sub-agents list. Available: %s", params.Agent, strings.Join(subAgentNames, ", "))), nil
		}
		return tools.ResultError(fmt.Sprintf("agent %q is not in the sub-agents list. This agent has no sub-agents configured.", params.Agent)), nil
	}

	// Enforce concurrency cap.
	if h.runningTaskCount() >= maxConcurrentTasks {
		return tools.ResultError(fmt.Sprintf("maximum concurrent background agents (%d) reached; stop or wait for existing tasks to complete", maxConcurrentTasks)), nil
	}

	// Enforce total cap, pruning finished tasks first.
	if h.totalTaskCount() >= maxTotalTasks {
		h.pruneCompleted()
		if h.totalTaskCount() >= maxTotalTasks {
			return tools.ResultError(fmt.Sprintf("maximum total background agents (%d) reached; view and discard old tasks first", maxTotalTasks)), nil
		}
	}

	sessionID := newSessionID()

	// taskCtx is detached from the parent message context so the task is
	// not killed when the parent message ctx is cancelled (e.g. user sends
	// a new message in the TUI). It is only canceled by HandleStop or
	// StopAll.
	taskCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	// needsRun is small but buffered so a synchronous Handler.HandleRun /
	// HandleSendMessage send doesn't block when the driver hasn't reached
	// its read yet. At any moment there is at most one outstanding signal
	// (because a resume can only happen after a previous run completed),
	// so capacity > 1 is purely defensive.
	needsRun := make(chan struct{}, 4)
	completed := make(chan *RunResult, 4)

	t := &task{
		sessionID: sessionID,
		agentName: params.Agent,
		taskDesc:  params.Task,
		cancel:    cancel,
		startTime: time.Now(),
		ctx:       taskCtx,
		needsRun:  needsRun,
	}
	t.storeStatus(taskRunning)
	h.tasks.Store(sessionID, t)

	slog.DebugContext(ctx, "Starting background agent", "session_id", sessionID, "agent", params.Agent)

	setup := h.runner.RunAgent(taskCtx, RunParams{
		AgentName:      params.Agent,
		Task:           params.Task,
		ExpectedOutput: params.ExpectedOutput,
		ParentSession:  sess,
		OnContent:      t.writeOutput,
		SessionID:      sessionID,
		OnRuntimeReady: func(rt TaskRuntime) {
			t.runtime = rt.Steerable
		},
		ResumeSignal: needsRun,
		Completed:    completed,
	})
	if setup != nil && setup.ErrMsg != "" {
		// Setup failed before the runtime could wire the task up. Mark
		// the task as failed and tear down; no completion will ever
		// arrive on the channel.
		t.errMsg = setup.ErrMsg
		t.storeStatus(taskFailed)
		cancel()
		slog.DebugContext(ctx, "Background agent setup failed", "session_id", sessionID, "agent", params.Agent, "error", setup.ErrMsg)
		return tools.ResultError("failed to set up background agent: " + setup.ErrMsg), nil
	}

	// Track task status by reading completion results. The goroutine
	// lives for the entire task lifetime so it can see both the initial
	// run's result and any resume runs that follow.
	h.wg.Go(func() {
		for {
			select {
			case result := <-completed:
				h.finishTaskRun(ctx, t, taskCtx, result)
			case <-taskCtx.Done():
				return
			}
		}
	})

	// Kick off the initial run. The send is non-blocking because needsRun
	// is buffered.
	needsRun <- struct{}{}

	return tools.ResultSuccess(fmt.Sprintf("Background agent started. Session ID: %s\nAgent: %s\nTask: %s",
		sessionID, params.Agent, params.Task)), nil
}

// finishTaskRun applies the status transition for one RunStream
// invocation. It is called from the result-tracking goroutine each time
// the driver pushes a completion. After a normal completion the task
// stays around for resume via send_message_background_agent.
func (h *Handler) finishTaskRun(ctx context.Context, t *task, taskCtx context.Context, result *RunResult) {
	if result == nil {
		return
	}
	if result.ErrMsg != "" {
		t.errMsg = result.ErrMsg
		t.storeStatus(taskFailed)
		slog.DebugContext(ctx, "Background agent failed", "session_id", t.sessionID, "agent", t.agentName, "error", result.ErrMsg)
		return
	}

	if taskCtx.Err() != nil && t.loadStatus() == taskRunning {
		t.storeStatus(taskStopped)
		slog.DebugContext(ctx, "Background agent stopped", "session_id", t.sessionID)
		return
	}

	// Write result before CAS so readers who observe taskCompleted
	// always see the populated result field.
	t.result = result.Result
	if t.casStatus(taskRunning, taskCompleted) {
		slog.DebugContext(ctx, "Background agent completed", "session_id", t.sessionID, "agent", t.agentName)
	}
}

// HandleList lists all background agent tasks.
func (h *Handler) HandleList(_ context.Context, _ *session.Session, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	var out strings.Builder
	out.WriteString("Background Agents:\n\n")

	var count int
	h.tasks.Range(func(_ string, t *task) bool {
		count++
		elapsed := time.Since(t.startTime).Round(time.Second)
		fmt.Fprintf(&out, "Session ID: %s\n", t.sessionID)
		fmt.Fprintf(&out, "  Agent:   %s\n", t.agentName)
		fmt.Fprintf(&out, "  Status:  %s\n", t.loadStatus())
		fmt.Fprintf(&out, "  Runtime: %s\n", elapsed)
		out.WriteString("\n")
		return true
	})

	if count == 0 {
		out.WriteString("No background agents found.\n")
	}

	return tools.ResultSuccess(out.String()), nil
}

// HandleView returns the output and status of a specific background agent task.
func (h *Handler) HandleView(_ context.Context, _ *session.Session, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params ViewBackgroundAgentArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	t, exists := h.tasks.Load(params.SessionID)
	if !exists {
		return tools.ResultError("background agent session not found: " + params.SessionID), nil
	}

	status := t.loadStatus()
	elapsed := time.Since(t.startTime).Round(time.Second)

	return tools.ResultSuccess(t.formatView(status, elapsed)), nil
}

// HandleSendMessage sends a follow-up or steering message to a
// background agent task by enqueuing it on the task's dedicated runtime
// steer queue.
//
// When the task is still running, the message is injected into the
// agent loop at its next natural steering point (between tool calls).
// When the task has gone idle after completing its last turn, the
// message is enqueued and the runtime driver is asked to re-run the
// sub-session by signalling needsRun.
func (h *Handler) HandleSendMessage(_ context.Context, _ *session.Session, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params SendMessageBackgroundAgentArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return tools.ResultError("session_id must not be empty"), nil
	}
	if strings.TrimSpace(params.Message) == "" {
		return tools.ResultError("message must not be empty"), nil
	}
	t, exists := h.tasks.Load(params.SessionID)
	if !exists {
		return tools.ResultError("background agent session not found: " + params.SessionID), nil
	}

	switch t.loadStatus() {
	case taskRunning:
		if t.runtime == nil {
			return tools.ResultError("background agent runtime not available yet"), nil
		}
		if err := t.runtime.Steer(params.Message); err != nil {
			return tools.ResultError("failed to send message: " + err.Error()), nil
		}

	case taskCompleted:
		if t.runtime == nil || t.needsRun == nil {
			return tools.ResultError("background agent runtime not available"), nil
		}
		if !t.casStatus(taskCompleted, taskRunning) {
			// Lost the race — another caller already transitioned the
			// task. Re-read and dispatch accordingly.
			if s := t.loadStatus(); s == taskRunning {
				if err := t.runtime.Steer(params.Message); err != nil {
					return tools.ResultError("failed to send message: " + err.Error()), nil
				}
				return tools.ResultSuccess(fmt.Sprintf("Message sent to background agent session %s.", params.SessionID)), nil
			}
			return tools.ResultError(fmt.Sprintf("background agent session %s cannot receive messages (status: %s)", params.SessionID, t.loadStatus())), nil
		}
		if err := t.runtime.Steer(params.Message); err != nil {
			// Roll back the status transition so the task stays
			// observable as completed if we can't deliver the message.
			t.storeStatus(taskCompleted)
			return tools.ResultError("failed to send message: " + err.Error()), nil
		}
		// Ask the driver to start a new RunStream invocation. needsRun
		// is buffered, so this never blocks under normal conditions.
		select {
		case t.needsRun <- struct{}{}:
		default:
			slog.Warn("background agent needsRun channel full; resume signal dropped", "session_id", params.SessionID)
		}

	default:
		return tools.ResultError(fmt.Sprintf("background agent session %s cannot receive messages (status: %s)", params.SessionID, t.loadStatus())), nil
	}

	return tools.ResultSuccess(fmt.Sprintf("Message sent to background agent session %s.", params.SessionID)), nil
}

// HandleStop cancels a running background agent task.
func (h *Handler) HandleStop(_ context.Context, _ *session.Session, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params StopBackgroundAgentArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	t, exists := h.tasks.Load(params.SessionID)
	if !exists {
		return tools.ResultError("background agent session not found: " + params.SessionID), nil
	}

	if !t.casStatus(taskRunning, taskStopped) {
		return tools.ResultError(fmt.Sprintf("background agent session %s is not running (status: %s)", params.SessionID, t.loadStatus())), nil
	}

	t.cancel()

	return tools.ResultSuccess(fmt.Sprintf("Background agent session %s stopped.", params.SessionID)), nil
}

// StopAll cancels every task's context (running or idle/completed) and
// waits for their goroutines to exit. Called during runtime shutdown
// to ensure clean teardown. Completed tasks survive past their initial
// run in case send_message resumes them, so we have to cancel them
// explicitly to terminate the result-tracker goroutines.
func (h *Handler) StopAll() {
	h.tasks.Range(func(_ string, t *task) bool {
		t.casStatus(taskRunning, taskStopped)
		if t.cancel != nil {
			t.cancel()
		}
		return true
	})
	h.wg.Wait()
}

// RegisterHandlers adds all background agent tool handlers to the given
// dispatch map, keyed by tool name.
func (h *Handler) RegisterHandlers(register func(name string, fn func(context.Context, *session.Session, tools.ToolCall) (*tools.ToolCallResult, error))) {
	register(ToolNameRunBackgroundAgent, h.HandleRun)
	register(ToolNameListBackgroundAgents, h.HandleList)
	register(ToolNameViewBackgroundAgent, h.HandleView)
	register(ToolNameStopBackgroundAgent, h.HandleStop)
	register(ToolNameSendMessageBackgroundAgent, h.HandleSendMessage)
}

// New returns a lightweight ToolSet for registering background agent
// tool definitions and instructions. It does not require a Runner and is
// suitable for use in the teamloader registry.
func New() tools.ToolSet {
	return &ToolSet{}
}

// ToolSet provides tool definitions and instructions without a Runner.
type ToolSet struct{}

func (t *ToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return backgroundAgentTools(), nil
}

func (t *ToolSet) Instructions() string {
	return `# Background Agents

Use background agents to dispatch work to sub-agents concurrently. Each background agent runs in its own session, identified by a stable session ID.

- **run_background_agent**: Start a command, returns the session ID. The sub-agent runs with all tools pre-approved — use only with trusted sub-agents and well-scoped tasks.
- **list_background_agents**: Show all background agents with status and runtime
- **view_background_agent**: Get output and status of a background agent by session_id
- **stop_background_agent**: Terminate a background agent by session_id
- **send_message_background_agent**: Send a follow-up or steering message to a background agent by session_id; the message is injected at the next natural steering point

**Notes**: Output capped at 10MB per agent. All background agents auto-terminate when the parent agent stops.`
}

func backgroundAgentTools() []tools.Tool {
	return []tools.Tool{
		{
			Name:     ToolNameRunBackgroundAgent,
			Category: "transfer",
			Description: `Start a sub-agent in the background and return immediately with the child's session ID.
Use this to dispatch work to multiple sub-agents concurrently. The sub-agent runs with all tools
pre-approved — use only with trusted sub-agents and well-scoped tasks. Check progress with
view_background_agent and collect results once the agent is done.`,
			Parameters:  tools.MustSchemaFor[RunBackgroundAgentArgs](),
			Annotations: tools.ToolAnnotations{Title: "Run Background Agent"},
		},
		{
			Name:        ToolNameListBackgroundAgents,
			Category:    "transfer",
			Description: `List all background agents with their status and runtime.`,
			Annotations: tools.ToolAnnotations{
				Title:        "List Background Agents",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameViewBackgroundAgent,
			Category:    "transfer",
			Description: `View the output and status of a specific background agent by session ID. Returns live buffered output if still running, or the final result if complete.`,
			Parameters:  tools.MustSchemaFor[ViewBackgroundAgentArgs](),
			Annotations: tools.ToolAnnotations{
				Title:        "View Background Agent",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameStopBackgroundAgent,
			Category:    "transfer",
			Description: `Stop a running background agent by session ID.`,
			Parameters:  tools.MustSchemaFor[StopBackgroundAgentArgs](),
			Annotations: tools.ToolAnnotations{
				Title: "Stop Background Agent",
			},
		},
		{
			Name:        ToolNameSendMessageBackgroundAgent,
			Category:    "transfer",
			Description: `Send a follow-up or steering message to a background agent by session ID. The message is injected into the agent's loop at its next natural steering point (between tool calls). Use this to redirect, correct, or provide additional context to a background agent without stopping it.`,
			Parameters:  tools.MustSchemaFor[SendMessageBackgroundAgentArgs](),
			Annotations: tools.ToolAnnotations{Title: "Send Message to Background Agent"},
		},
	}
}

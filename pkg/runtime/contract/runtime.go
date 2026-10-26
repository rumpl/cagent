// Package contract defines the common local and remote runtime surface.
package contract

import (
	"context"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/types"
	"github.com/docker/docker-agent/pkg/effort"
	"github.com/docker/docker-agent/pkg/runtime/modelcatalog"
	"github.com/docker/docker-agent/pkg/runtime/toolexec"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/sessiontitle"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/skills"
)

type (
	ModelChoice   = modelcatalog.ModelChoice
	ResumeRequest = toolexec.ResumeRequest
)

// Runtime defines the contract for runtime execution
type Runtime interface {
	// CurrentAgentInfo returns information about the currently active agent
	CurrentAgentInfo(ctx context.Context) CurrentAgentInfo
	// CurrentAgentName returns the name of the currently active agent
	CurrentAgentName(ctx context.Context) string
	// SetCurrentAgent sets the currently active agent for subsequent user messages
	SetCurrentAgent(ctx context.Context, agentName string) error
	// CurrentAgentTools returns the tools for the active agent
	CurrentAgentTools(ctx context.Context) ([]tools.Tool, error)
	// CurrentAgentToolsetStatuses returns lifecycle status for each toolset of
	// the active agent (name, kind, state, last error, restart count).
	// Used by the /tools dialog. Best-effort: toolsets that don't expose
	// state appear with State == StateStopped/Ready as appropriate.
	CurrentAgentToolsetStatuses() []tools.ToolsetStatus

	// RestartToolset finds the named toolset on the active agent and asks
	// its supervisor to drop the current session and reconnect. Returns
	// an error if no toolset matches name or the toolset does not
	// implement tools.Restartable.
	RestartToolset(ctx context.Context, name string) error
	// EmitStartupInfo emits initial agent, team, and toolset information for immediate display.
	// When sess is non-nil and contains token data, a TokenUsageEvent is also emitted
	// so the UI can display context usage percentage on session restore.
	EmitStartupInfo(ctx context.Context, sess *session.Session, events EventSink)
	// EmitAgentInfo emits up-to-date agent and team info (model, thinking
	// level, description) without re-running heavier startup work such as
	// tool discovery. Used to refresh the UI after a lightweight change like
	// cycling the thinking level, avoiding the tool-count flicker that
	// re-emitting full startup info would cause.
	EmitAgentInfo(ctx context.Context, events EventSink)
	// ResetStartupInfo resets the startup info emission flag, allowing re-emission
	ResetStartupInfo()
	// RunStream starts the agent's interaction loop and returns a channel of events.
	// Consumers must drain it to closure before releasing turn ownership, even
	// after cancellation. Cancel the supplied context before draining on errors.
	RunStream(ctx context.Context, sess *session.Session) <-chan Event
	// Run starts the agent's interaction loop and returns the final messages
	Run(ctx context.Context, sess *session.Session) ([]session.Message, error)
	// Resume allows resuming execution after user confirmation.
	// The ResumeRequest carries the decision type and an optional reason (for rejections).
	Resume(ctx context.Context, req ResumeRequest)
	// ResumeElicitation sends an elicitation response back to a waiting
	// elicitation request. elicitationID correlates the response with a
	// specific concurrent request (see ElicitationRequestEvent.ElicitationID);
	// omit it (or pass "") to fall back to resolving the sole pending
	// request, for backward compatibility with older clients. The parameter
	// is variadic — rather than a required 4th positional argument — purely
	// so pre-#3584 callers of this interface method keep compiling unchanged;
	// implementations should treat more than one value as a caller error and
	// use only the first.
	ResumeElicitation(_ context.Context, action tools.ElicitationAction, content map[string]any, elicitationID ...string) error
	// SessionStore returns the session store for browsing/loading past sessions.
	// Returns nil if no persistent session store is configured.
	SessionStore() session.Store

	// Summarize generates a summary for the session
	Summarize(ctx context.Context, sess *session.Session, additionalPrompt string, events EventSink)

	// PermissionsInfo returns the team-level permission patterns (allow/ask/deny).
	// Returns nil if no permissions are configured.
	PermissionsInfo() *PermissionsInfo

	// ReadSkillContent loads a skill for sess without executing embedded commands.
	ReadSkillContent(ctx context.Context, sess *session.Session, name string) (string, error)

	// CurrentAgentSkillsToolset returns the skills toolset for the current agent, or nil if skills are not enabled.
	CurrentAgentSkillsToolset() *skills.ToolSet

	// RunSkillFork executes a `context: fork` skill as an isolated
	// sub-session of sess. Used by the run_skill tool and the App's
	// slash-command path. Returns a ToolCallResult carrying the sub-agent's
	// last assistant message, or a user-facing error reason; err is non-nil
	// only for unexpected runtime failures.
	RunSkillFork(ctx context.Context, sess *session.Session, args skills.RunSkillArgs, events EventSink) (*tools.ToolCallResult, error)

	// CurrentMCPPrompts returns MCP prompts available from the current agent's toolsets.
	// Returns an empty map if no MCP prompts are available.
	CurrentMCPPrompts(ctx context.Context) map[string]tools.PromptInfo

	// ExecuteMCPPrompt executes a named MCP prompt with the given arguments.
	ExecuteMCPPrompt(ctx context.Context, promptName string, arguments map[string]string) (string, error)

	// UpdateSessionTitle persists a new title for the current session.
	UpdateSessionTitle(ctx context.Context, sess *session.Session, title string) error

	// TitleGenerator returns a generator for automatic session titles, or nil
	// if the runtime does not support local title generation (e.g. remote
	// runtimes) or no configured model is a usable title candidate.
	TitleGenerator(ctx context.Context) *sessiontitle.Generator

	// Steer enqueues a user message for urgent mid-turn injection into the
	// running agent loop. Returns an error if the queue is full or steering
	// is not available.
	Steer(ctx context.Context, msg QueuedMessage) error
	// FollowUp enqueues a message for end-of-turn processing. Each follow-up
	// gets a full undivided agent turn. Returns an error if the queue is full.
	FollowUp(ctx context.Context, msg QueuedMessage) error

	// SetAgentModel sets a model override for the named agent.
	// modelRef can be:
	//   - "" (empty) to clear the override and use the agent's default model
	//   - A model name from the config (e.g., "my_fast_model")
	//   - An inline spec (e.g., "openai/gpt-4o")
	// Returns [ErrUnsupported] for runtimes that don't expose model
	// switching (e.g. remote runtimes, where the server owns the choice).
	SetAgentModel(ctx context.Context, agentName, modelRef string) error

	// CycleAgentThinkingLevel advances the named agent's thinking-effort
	// level to the next value in its provider's cycle (wrapping around),
	// applies it as a runtime model override, and returns the newly
	// selected level. Returns [ErrUnsupported] for runtimes that can't
	// switch models (e.g. remote runtimes) or for models that don't
	// support thinking-effort selection.
	CycleAgentThinkingLevel(ctx context.Context, agentName string) (effort.Level, error)

	// SetAgentThinkingLevel sets the named agent's thinking-effort level to
	// the requested value, applies it as a runtime model override, and
	// returns the applied level. The level must be one the current model
	// supports. Returns [ErrUnsupported] for runtimes that can't switch
	// models (e.g. remote runtimes) or for models that don't support
	// thinking-effort selection.
	SetAgentThinkingLevel(ctx context.Context, agentName string, level effort.Level) (effort.Level, error)

	// AvailableModels returns the models the user can pick from in the
	// /model picker. Returns nil for runtimes that don't expose model
	// switching; see SupportsModelSwitching for a cheap pre-check.
	AvailableModels(ctx context.Context) []ModelChoice

	// SupportsModelSwitching reports whether SetAgentModel and
	// AvailableModels are wired for this runtime. Use it to gate UI
	// affordances (e.g. show /model in the menu) without paying the
	// cost of AvailableModels.
	SupportsModelSwitching() bool

	// OnToolsChanged registers a handler invoked outside of any RunStream
	// when a toolset reports a tool list change (e.g. after an MCP
	// ToolListChanged notification). A nil handler unregisters. Runtimes
	// that don't emit such events can implement this as a no-op.
	OnToolsChanged(handler func(Event))

	// OnBackgroundEvent registers a handler invoked outside of any RunStream
	// for events surfaced from detached background work (e.g. token usage
	// from background agent tasks started via run_background_agent), which
	// has no live event channel of its own. Runtimes that don't run local
	// background work can implement this as a no-op.
	OnBackgroundEvent(handler func(Event))

	// OnElicitationRequest registers a handler invoked whenever an MCP
	// toolset raises an elicitation request, regardless of which stream
	// (foreground or a detached background job) is currently "active" for
	// the runtime's internal elicitation bridge. This is the reliable
	// delivery route for background-job elicitations (#3584); runtimes
	// that don't run local background work can implement this as a no-op.
	OnElicitationRequest(handler func(Event))

	// QueueStatus returns the current depth and capacity of message queues
	QueueStatus() QueueStatus

	// TogglePause toggles whether the run loop is paused at iteration
	// boundaries. Returns the new state (true if now paused). Returns
	// [ErrUnsupported] for runtimes that don't expose pause control.
	TogglePause(ctx context.Context) (paused bool, err error)

	// Close releases resources held by the runtime (e.g., background
	// agents and pending lifecycles). The session store is *not* closed
	// here: it is supplied by the embedder via WithSessionStore and may
	// be shared with other runtimes (e.g. the TUI's spawner reuses the
	// same store across spawned sessions). Embedders that own the store
	// must close it themselves.
	Close() error
}

// PermissionsInfo contains the allow, ask, and deny patterns for tool permissions.
type PermissionsInfo struct {
	Allow []string
	Ask   []string
	Deny  []string
}

type CurrentAgentInfo struct {
	Name        string
	Description string
	Commands    types.Commands
}

type QueuedMessage struct {
	ID           string
	Content      string
	MultiContent []chat.MessagePart
}

type QueueStatus struct {
	SteerDepth       int
	SteerCapacity    int
	FollowupDepth    int
	FollowupCapacity int
}

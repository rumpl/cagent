package runtime

import (
	"context"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// RuntimeLifecycleHooks groups the typed lifecycle hooks exposed by the runtime.
// Hooks mutate typed phase objects instead of reaching into random runtime internals.
type RuntimeLifecycleHooks struct {
	SessionStart          []SessionHook
	SessionEnd            []SessionHook
	BeforeUserMessage     []UserMessageHook
	AfterUserMessage      []UserMessageHook
	TurnStart             []TurnHook
	TurnEnd               []TurnHook
	BeforePauseForUser    []PauseHook
	AfterResume           []ResumeHook
	BeforeAssistantCommit []AssistantCommitHook
	AfterAssistantCommit  []AssistantCommitHook
	BeforeToolBatch       []ToolBatchHook
	AfterToolBatch        []ToolBatchHook
}

type SessionHook func(context.Context, *SessionPhase) error
type UserMessageHook func(context.Context, *UserMessagePhase) error
type TurnHook func(context.Context, *TurnPhase) error
type PauseHook func(context.Context, *PausePhase) error
type ResumeHook func(context.Context, *ResumePhase) error
type AssistantCommitHook func(context.Context, *AssistantCommitPhase) error
type ToolBatchHook func(context.Context, *ToolBatchPhase) error

type BuildContextHandler func(context.Context, *BuildContextPhase) error
type BuildContextMiddleware func(context.Context, *BuildContextPhase, BuildContextHandler) error

type ModelHandler func(context.Context, *ModelPhase) (*ModelResult, error)
type ModelMiddleware func(context.Context, *ModelPhase, ModelHandler) (*ModelResult, error)

type ToolHandler func(context.Context, *ToolCallPhase) (*ToolExecutionResult, error)
type ToolMiddleware func(context.Context, *ToolCallPhase, ToolHandler) (*ToolExecutionResult, error)

// SessionPhase describes a session lifecycle boundary.
type SessionPhase struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Events    chan Event
}

// UserMessagePhase describes a user input passing through the runtime.
type UserMessagePhase struct {
	Runtime      *LocalRuntime
	Execution    *Execution
	Session      *session.Session
	Agent        *agent.Agent
	Events       chan Event
	Input        *userInputAction
	DisplayInput string
}

// TurnPhase describes a turn lifecycle boundary.
type TurnPhase struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Turn      *Turn
	Events    chan Event
}

// PausePhase describes a runtime pause that requires user input.
type PausePhase struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Events    chan Event
	Reason    string
	ToolCall  *tools.ToolCall
	Tool      *tools.Tool
}

// ResumePhase describes a runtime resume after a pause.
type ResumePhase struct {
	Runtime     *LocalRuntime
	Execution   *Execution
	Session     *session.Session
	Agent       *agent.Agent
	Events      chan Event
	Reason      string
	Request     ResumeRequest
	Elicitation *ElicitationResult
}

// BuildContextPhase describes prompt-context construction for a turn.
type BuildContextPhase struct {
	Runtime         *LocalRuntime
	Execution       *Execution
	Session         *session.Session
	Agent           *agent.Agent
	Turn            *Turn
	Model           provider.Provider
	ModelDefinition *modelsdev.Model
	PromptContext   *session.PromptContext
}

// ModelPhase describes a model invocation for a turn.
type ModelPhase struct {
	Runtime         *LocalRuntime
	Execution       *Execution
	Session         *session.Session
	Agent           *agent.Agent
	Turn            *Turn
	PromptContext   *session.PromptContext
	Messages        []chat.Message
	Tools           []tools.Tool
	Model           provider.Provider
	ModelDefinition *modelsdev.Model
	Events          chan Event
}

// ModelResult is the typed result of a model phase.
type ModelResult struct {
	Result    streamResult
	UsedModel provider.Provider
}

// AssistantCommitPhase describes committing the assistant message for a turn.
type AssistantCommitPhase struct {
	Runtime         *LocalRuntime
	Execution       *Execution
	Session         *session.Session
	Agent           *agent.Agent
	Turn            *Turn
	Events          chan Event
	Result          streamResult
	AgentTools      []tools.Tool
	ModelID         string
	ModelDefinition *modelsdev.Model
	MessageUsage    *MessageUsage
}

// ToolBatchPhase describes a batch of tool calls requested in a turn.
type ToolBatchPhase struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Turn      *Turn
	Events    chan Event
	Calls     []tools.ToolCall
	Tools     []tools.Tool
}

// ToolCallPhase describes a single tool call execution.
type ToolCallPhase struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Turn      *Turn
	Events    chan Event
	ToolCall  tools.ToolCall
	Tool      tools.Tool
}

// ToolExecutionResult is the typed result of a tool middleware chain.
type ToolExecutionResult struct {
	Canceled bool
}

func mergeLifecycleHooks(dst *RuntimeLifecycleHooks, src RuntimeLifecycleHooks) {
	dst.SessionStart = append(dst.SessionStart, src.SessionStart...)
	dst.SessionEnd = append(dst.SessionEnd, src.SessionEnd...)
	dst.BeforeUserMessage = append(dst.BeforeUserMessage, src.BeforeUserMessage...)
	dst.AfterUserMessage = append(dst.AfterUserMessage, src.AfterUserMessage...)
	dst.TurnStart = append(dst.TurnStart, src.TurnStart...)
	dst.TurnEnd = append(dst.TurnEnd, src.TurnEnd...)
	dst.BeforePauseForUser = append(dst.BeforePauseForUser, src.BeforePauseForUser...)
	dst.AfterResume = append(dst.AfterResume, src.AfterResume...)
	dst.BeforeAssistantCommit = append(dst.BeforeAssistantCommit, src.BeforeAssistantCommit...)
	dst.AfterAssistantCommit = append(dst.AfterAssistantCommit, src.AfterAssistantCommit...)
	dst.BeforeToolBatch = append(dst.BeforeToolBatch, src.BeforeToolBatch...)
	dst.AfterToolBatch = append(dst.AfterToolBatch, src.AfterToolBatch...)
}

func (r *LocalRuntime) runSessionHooks(ctx context.Context, hooks []SessionHook, phase *SessionPhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runUserMessageHooks(ctx context.Context, hooks []UserMessageHook, phase *UserMessagePhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runTurnHooks(ctx context.Context, hooks []TurnHook, phase *TurnPhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runPauseHooks(ctx context.Context, hooks []PauseHook, phase *PausePhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runResumeHooks(ctx context.Context, hooks []ResumeHook, phase *ResumePhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runAssistantCommitHooks(ctx context.Context, hooks []AssistantCommitHook, phase *AssistantCommitPhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) runToolBatchHooks(ctx context.Context, hooks []ToolBatchHook, phase *ToolBatchPhase) error {
	for _, hook := range hooks {
		if err := hook(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *LocalRuntime) buildContext(ctx context.Context, phase *BuildContextPhase) error {
	final := func(_ context.Context, phase *BuildContextPhase) error {
		phase.PromptContext = phase.Session.BuildBasePromptContext(phase.Agent)
		if phase.ModelDefinition != nil && len(phase.ModelDefinition.Modalities.Input) > 0 {
			if !containsModality(phase.ModelDefinition.Modalities.Input, "image") {
				phase.PromptContext.Messages = stripImageContent(phase.PromptContext.Messages)
			}
		}
		return nil
	}

	for i := len(r.buildContextMiddlewares) - 1; i >= 0; i-- {
		next := final
		middleware := r.buildContextMiddlewares[i]
		final = func(ctx context.Context, phase *BuildContextPhase) error {
			return middleware(ctx, phase, next)
		}
	}

	return final(ctx, phase)
}

func (r *LocalRuntime) executeModelPhase(ctx context.Context, phase *ModelPhase) (*ModelResult, error) {
	final := func(ctx context.Context, phase *ModelPhase) (*ModelResult, error) {
		stream, err := phase.Model.CreateChatCompletionStream(ctx, phase.Messages, phase.Tools)
		if err != nil {
			return nil, err
		}
		if rp, ok := phase.Model.(interface{ LastSelectedModelID() string }); ok {
			if selected := rp.LastSelectedModelID(); selected != "" {
				phase.Events <- AgentInfo(phase.Agent.Name(), selected, phase.Agent.Description(), phase.Agent.WelcomeMessage())
			}
		}
		res, err := r.handleStream(ctx, stream, phase.Agent, phase.Tools, phase.Session, phase.ModelDefinition, phase.Events)
		if err != nil {
			return nil, err
		}
		return &ModelResult{Result: res, UsedModel: phase.Model}, nil
	}

	for i := len(r.modelMiddlewares) - 1; i >= 0; i-- {
		next := final
		middleware := r.modelMiddlewares[i]
		final = func(ctx context.Context, phase *ModelPhase) (*ModelResult, error) {
			return middleware(ctx, phase, next)
		}
	}

	return final(ctx, phase)
}

func (r *LocalRuntime) executeToolPhase(ctx context.Context, phase *ToolCallPhase, final ToolHandler) (*ToolExecutionResult, error) {
	if final == nil {
		final = func(context.Context, *ToolCallPhase) (*ToolExecutionResult, error) {
			return &ToolExecutionResult{}, nil
		}
	}

	wrapped := final
	for i := len(r.toolMiddlewares) - 1; i >= 0; i-- {
		next := wrapped
		middleware := r.toolMiddlewares[i]
		wrapped = func(ctx context.Context, phase *ToolCallPhase) (*ToolExecutionResult, error) {
			return middleware(ctx, phase, next)
		}
	}

	return wrapped(ctx, phase)
}

func containsModality(modalities []string, want string) bool {
	for _, modality := range modalities {
		if modality == want {
			return true
		}
	}
	return false
}

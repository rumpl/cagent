package runtime

import (
	"context"
	"log/slog"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/session"
)

// RuntimeObservers groups side-effect observers that react to typed state
// changes without controlling the execution loop.
type RuntimeObservers struct {
	UserMessageAdded    []UserMessageObserver
	AssistantChunk      []AssistantChunkObserver
	MessageAdded        []MessageObserver
	SessionSummaryAdded []SessionSummaryObserver
	TokenUsageUpdated   []TokenUsageObserver
	SubSessionCompleted []SubSessionObserver
	SessionTitleUpdated []SessionTitleObserver
	Notifications       []NotificationObserver
}

type UserMessageObserver func(context.Context, *ObservedUserMessage) error
type AssistantChunkObserver func(context.Context, *ObservedAssistantChunk) error
type MessageObserver func(context.Context, *ObservedMessage) error
type SessionSummaryObserver func(context.Context, *ObservedSessionSummary) error
type TokenUsageObserver func(context.Context, *ObservedTokenUsage) error
type SubSessionObserver func(context.Context, *ObservedSubSession) error
type SessionTitleObserver func(context.Context, *ObservedSessionTitle) error
type NotificationObserver func(context.Context, *ObservedNotification) error

// ObservedUserMessage describes a user message that entered a session through
// the runtime's canonical input path.
type ObservedUserMessage struct {
	Runtime         *LocalRuntime
	Execution       *Execution
	Session         *session.Session
	Agent           *agent.Agent
	Message         *session.Message
	SessionPosition int
}

// ObservedAssistantChunk describes a streaming assistant delta for persistence
// or presentation observers.
type ObservedAssistantChunk struct {
	Runtime        *LocalRuntime
	Execution      *Execution
	Session        *session.Session
	Agent          *agent.Agent
	ContentDelta   string
	ReasoningDelta string
}

// ObservedMessage describes a committed transcript message.
type ObservedMessage struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Message   *session.Message
}

// ObservedSessionSummary describes a summary committed during compaction.
type ObservedSessionSummary struct {
	Runtime        *LocalRuntime
	Session        *session.Session
	Agent          *agent.Agent
	Summary        string
	FirstKeptEntry int
}

// ObservedTokenUsage describes token/cost totals after a state change.
type ObservedTokenUsage struct {
	Runtime   *LocalRuntime
	Execution *Execution
	Session   *session.Session
	Agent     *agent.Agent
	Usage     *Usage
}

// ObservedSubSession describes a completed child session embedded in a parent.
type ObservedSubSession struct {
	Runtime       *LocalRuntime
	ParentSession *session.Session
	SubSession    *session.Session
	Agent         *agent.Agent
}

// ObservedSessionTitle describes a persisted title change.
type ObservedSessionTitle struct {
	Runtime *LocalRuntime
	Session *session.Session
	Title   string
}

// ObservedNotification describes a user-facing warning or error emitted by the runtime.
type ObservedNotification struct {
	Runtime   *LocalRuntime
	Agent     *agent.Agent
	SessionID string
	Level     string
	Message   string
}

func mergeRuntimeObservers(dst *RuntimeObservers, src RuntimeObservers) {
	dst.UserMessageAdded = append(dst.UserMessageAdded, src.UserMessageAdded...)
	dst.AssistantChunk = append(dst.AssistantChunk, src.AssistantChunk...)
	dst.MessageAdded = append(dst.MessageAdded, src.MessageAdded...)
	dst.SessionSummaryAdded = append(dst.SessionSummaryAdded, src.SessionSummaryAdded...)
	dst.TokenUsageUpdated = append(dst.TokenUsageUpdated, src.TokenUsageUpdated...)
	dst.SubSessionCompleted = append(dst.SubSessionCompleted, src.SubSessionCompleted...)
	dst.SessionTitleUpdated = append(dst.SessionTitleUpdated, src.SessionTitleUpdated...)
	dst.Notifications = append(dst.Notifications, src.Notifications...)
}

func observeList[T any, F ~func(context.Context, *T) error](ctx context.Context, observers []F, value *T, kind string) {
	for _, observer := range observers {
		if err := observer(ctx, value); err != nil {
			slog.Warn("Runtime observer failed", "kind", kind, "error", err)
		}
	}
}

func (r *LocalRuntime) observeUserMessage(ctx context.Context, observed *ObservedUserMessage) {
	observeList(ctx, r.observers.UserMessageAdded, observed, "user_message")
}

func (r *LocalRuntime) observeAssistantChunk(ctx context.Context, observed *ObservedAssistantChunk) {
	observeList(ctx, r.observers.AssistantChunk, observed, "assistant_chunk")
}

func (r *LocalRuntime) observeMessageAdded(ctx context.Context, observed *ObservedMessage) {
	observeList(ctx, r.observers.MessageAdded, observed, "message_added")
}

func (r *LocalRuntime) observeSessionSummary(ctx context.Context, observed *ObservedSessionSummary) {
	observeList(ctx, r.observers.SessionSummaryAdded, observed, "session_summary")
}

func (r *LocalRuntime) observeTokenUsage(ctx context.Context, observed *ObservedTokenUsage) {
	observeList(ctx, r.observers.TokenUsageUpdated, observed, "token_usage")
}

func (r *LocalRuntime) observeSubSessionCompleted(ctx context.Context, observed *ObservedSubSession) {
	observeList(ctx, r.observers.SubSessionCompleted, observed, "sub_session")
}

func (r *LocalRuntime) observeSessionTitle(ctx context.Context, observed *ObservedSessionTitle) {
	observeList(ctx, r.observers.SessionTitleUpdated, observed, "session_title")
}

func (r *LocalRuntime) observeNotification(ctx context.Context, observed *ObservedNotification) {
	observeList(ctx, r.observers.Notifications, observed, "notification")
}

package runtime

import (
	"time"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

func newAgentContext(name string) AgentContext {
	return AgentContext{AgentName: name, Timestamp: time.Now()}
}

// Event is part of the shared runtime event contract.
type Event = contract.Event

// SessionScoped is part of the shared runtime event contract.
type SessionScoped = contract.SessionScoped

// AgentContext is part of the shared runtime event contract.
type AgentContext = contract.AgentContext

// UserMessageEvent is part of the shared runtime event contract.
type UserMessageEvent = contract.UserMessageEvent

// PartialToolCallEvent is part of the shared runtime event contract.
type PartialToolCallEvent = contract.PartialToolCallEvent

// ToolCallEvent is part of the shared runtime event contract.
type ToolCallEvent = contract.ToolCallEvent

// ToolCallConfirmationEvent is part of the shared runtime event contract.
type ToolCallConfirmationEvent = contract.ToolCallConfirmationEvent

// ToolCallOutputEvent is part of the shared runtime event contract.
type ToolCallOutputEvent = contract.ToolCallOutputEvent

// ToolCallResponseEvent is part of the shared runtime event contract.
type ToolCallResponseEvent = contract.ToolCallResponseEvent

// BackgroundAgentStatusEvent is part of the shared runtime event contract.
type BackgroundAgentStatusEvent = contract.BackgroundAgentStatusEvent

// StreamStartedEvent is part of the shared runtime event contract.
type StreamStartedEvent = contract.StreamStartedEvent

// AgentChoiceEvent is part of the shared runtime event contract.
type AgentChoiceEvent = contract.AgentChoiceEvent

// AgentChoiceReasoningEvent is part of the shared runtime event contract.
type AgentChoiceReasoningEvent = contract.AgentChoiceReasoningEvent

// ErrorEvent is part of the shared runtime event contract.
type ErrorEvent = contract.ErrorEvent

// ShellOutputEvent is part of the shared runtime event contract.
type ShellOutputEvent = contract.ShellOutputEvent

// WarningEvent is part of the shared runtime event contract.
type WarningEvent = contract.WarningEvent

// ModelFallbackEvent is part of the shared runtime event contract.
type ModelFallbackEvent = contract.ModelFallbackEvent

// TokenUsageEvent is part of the shared runtime event contract.
type TokenUsageEvent = contract.TokenUsageEvent

// Usage is part of the shared runtime event contract.
type Usage = contract.Usage

// MessageUsage is part of the shared runtime event contract.
type MessageUsage = contract.MessageUsage

// SessionTitleEvent is part of the shared runtime event contract.
type SessionTitleEvent = contract.SessionTitleEvent

// PlanChangedEvent is part of the shared runtime event contract.
type PlanChangedEvent = contract.PlanChangedEvent

// SessionSummaryEvent is part of the shared runtime event contract.
type SessionSummaryEvent = contract.SessionSummaryEvent

// SessionCompactionEvent is part of the shared runtime event contract.
type SessionCompactionEvent = contract.SessionCompactionEvent

// StreamStoppedEvent is part of the shared runtime event contract.
type StreamStoppedEvent = contract.StreamStoppedEvent

// SessionRecoveredEvent is part of the shared runtime event contract.
type SessionRecoveredEvent = contract.SessionRecoveredEvent

// PausedEvent is part of the shared runtime event contract.
type PausedEvent = contract.PausedEvent

// ElicitationRequestEvent is part of the shared runtime event contract.
type ElicitationRequestEvent = contract.ElicitationRequestEvent

// ElicitationClosedEvent is part of the shared runtime event contract.
type ElicitationClosedEvent = contract.ElicitationClosedEvent

// AuthorizationEvent is part of the shared runtime event contract.
type AuthorizationEvent = contract.AuthorizationEvent

// MaxIterationsReachedEvent is part of the shared runtime event contract.
type MaxIterationsReachedEvent = contract.MaxIterationsReachedEvent

// BudgetUsageEvent is part of the shared runtime event contract.
type BudgetUsageEvent = contract.BudgetUsageEvent

// BudgetStatus is part of the shared runtime event contract.
type BudgetStatus = contract.BudgetStatus

// AgentBudgetUsage is part of the shared runtime event contract.
type AgentBudgetUsage = contract.AgentBudgetUsage

// BudgetExceededEvent is part of the shared runtime event contract.
type BudgetExceededEvent = contract.BudgetExceededEvent

// BudgetStopMessage is part of the shared runtime event contract.
type BudgetStopMessage = contract.BudgetStopMessage

// BudgetStopChatMessage is part of the shared runtime event contract.
type BudgetStopChatMessage = contract.BudgetStopChatMessage

// MCPInitStartedEvent is part of the shared runtime event contract.
type MCPInitStartedEvent = contract.MCPInitStartedEvent

// MCPInitFinishedEvent is part of the shared runtime event contract.
type MCPInitFinishedEvent = contract.MCPInitFinishedEvent

// AgentInfoEvent is part of the shared runtime event contract.
type AgentInfoEvent = contract.AgentInfoEvent

// AgentDetails is part of the shared runtime event contract.
type AgentDetails = contract.AgentDetails

// TeamInfoEvent is part of the shared runtime event contract.
type TeamInfoEvent = contract.TeamInfoEvent

// AgentSwitchingEvent is part of the shared runtime event contract.
type AgentSwitchingEvent = contract.AgentSwitchingEvent

// ToolsetInfoEvent is part of the shared runtime event contract.
type ToolsetInfoEvent = contract.ToolsetInfoEvent

// RAGIndexingStartedEvent is part of the shared runtime event contract.
type RAGIndexingStartedEvent = contract.RAGIndexingStartedEvent

// RAGIndexingProgressEvent is part of the shared runtime event contract.
type RAGIndexingProgressEvent = contract.RAGIndexingProgressEvent

// RAGIndexingCompletedEvent is part of the shared runtime event contract.
type RAGIndexingCompletedEvent = contract.RAGIndexingCompletedEvent

// HookStartedEvent is part of the shared runtime event contract.
type HookStartedEvent = contract.HookStartedEvent

// HookFinishedEvent is part of the shared runtime event contract.
type HookFinishedEvent = contract.HookFinishedEvent

// HookBlockedEvent is part of the shared runtime event contract.
type HookBlockedEvent = contract.HookBlockedEvent

// MessageAddedEvent is part of the shared runtime event contract.
type MessageAddedEvent = contract.MessageAddedEvent

// SubSessionCompletedEvent is part of the shared runtime event contract.
type SubSessionCompletedEvent = contract.SubSessionCompletedEvent

// AgentRouteEvent is part of the shared runtime event contract.
type AgentRouteEvent = contract.AgentRouteEvent

// RoutingDecisionEvent is part of the shared runtime event contract.
type RoutingDecisionEvent = contract.RoutingDecisionEvent

// EvaluationUsageEvent is part of the shared runtime event contract.
type EvaluationUsageEvent = contract.EvaluationUsageEvent

const StreamStopReasonMaxIterations = contract.StreamStopReasonMaxIterations

const (
	ErrorCodeModelError             = contract.ErrorCodeModelError
	ErrorCodeRateLimited            = contract.ErrorCodeRateLimited
	ErrorCodeContextExceeded        = contract.ErrorCodeContextExceeded
	ErrorCodeRequestTooLarge        = contract.ErrorCodeRequestTooLarge
	ErrorCodeMediaTooLarge          = contract.ErrorCodeMediaTooLarge
	ErrorCodeToolFailed             = contract.ErrorCodeToolFailed
	ErrorCodeHookBlocked            = contract.ErrorCodeHookBlocked
	ErrorCodeLoopDetected           = contract.ErrorCodeLoopDetected
	ErrorCodeStructuredOutputFailed = contract.ErrorCodeStructuredOutputFailed
	ErrorCodeRoutingFailed          = contract.ErrorCodeRoutingFailed
	CompactionOutcomeApplied        = contract.CompactionOutcomeApplied
	CompactionOutcomeSkipped        = contract.CompactionOutcomeSkipped
	CompactionOutcomeFailed         = contract.CompactionOutcomeFailed
)

// UserMessage constructs a runtime event.
func UserMessage(message, sessionID string, multiContent []chat.MessagePart, sessionPos ...int) Event {
	return contract.UserMessage(message, sessionID, multiContent, sessionPos...)
}

// PartialToolCall constructs a runtime event.
func PartialToolCall(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string) Event {
	return contract.PartialToolCall(toolCall, toolDefinition, agentName)
}

// ToolCall constructs a runtime event.
func ToolCall(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string) Event {
	return contract.ToolCall(toolCall, toolDefinition, agentName)
}

// ToolCallConfirmation constructs a runtime event.
func ToolCallConfirmation(toolCall tools.ToolCall, toolDefinition tools.Tool, agentName string, metadata map[string]string) Event {
	return contract.ToolCallConfirmation(toolCall, toolDefinition, agentName, metadata)
}

// ToolCallOutput constructs a runtime event.
func ToolCallOutput(toolCallID string, toolDefinition tools.Tool, output, agentName string) Event {
	return contract.ToolCallOutput(toolCallID, toolDefinition, output, agentName)
}

// ToolCallResponse constructs a runtime event.
func ToolCallResponse(toolCallID string, toolDefinition tools.Tool, result *tools.ToolCallResult, response, agentName string) Event {
	return contract.ToolCallResponse(toolCallID, toolDefinition, result, response, agentName)
}

// BackgroundAgentStatus constructs a runtime event.
func BackgroundAgentStatus(sessionID, agentName string, running bool) Event {
	return contract.BackgroundAgentStatus(sessionID, agentName, running)
}

// StreamStarted constructs a runtime event.
func StreamStarted(sessionID, agentName string) Event {
	return contract.StreamStarted(sessionID, agentName)
}

// AgentChoice constructs a runtime event.
func AgentChoice(agentName, sessionID, content string, messageID ...string) Event {
	return contract.AgentChoice(agentName, sessionID, content, messageID...)
}

// AgentChoiceReasoning constructs a runtime event.
func AgentChoiceReasoning(agentName, sessionID, content string, messageID ...string) Event {
	return contract.AgentChoiceReasoning(agentName, sessionID, content, messageID...)
}

// Error constructs a runtime event.
func Error(msg string) Event { return contract.Error(msg) }

// ErrorWithCode constructs a runtime event.
func ErrorWithCode(code, msg string) Event { return contract.ErrorWithCode(code, msg) }

// ErrorForSession constructs a runtime event.
func ErrorForSession(sessionID, msg string) Event { return contract.ErrorForSession(sessionID, msg) }

// ErrorWithCodeForSession constructs a runtime event.
func ErrorWithCodeForSession(sessionID, code, msg string) Event {
	return contract.ErrorWithCodeForSession(sessionID, code, msg)
}

// ShellOutput constructs a runtime event.
func ShellOutput(output string) Event { return contract.ShellOutput(output) }

// ShellCommandStarted constructs a runtime event.
func ShellCommandStarted(id, command string) Event { return contract.ShellCommandStarted(id, command) }

// ShellCommandOutput constructs a runtime event.
func ShellCommandOutput(id, output string) Event { return contract.ShellCommandOutput(id, output) }

// ShellCommandFinished constructs a runtime event.
func ShellCommandFinished(id, command, output string, err error) Event {
	return contract.ShellCommandFinished(id, command, output, err)
}

// Warning constructs a runtime event.
func Warning(message, agentName string) Event { return contract.Warning(message, agentName) }

// ModelFallback constructs a runtime event.
func ModelFallback(agentName, failedModel, fallbackModel, reason string, attempt, maxAttempts int) Event {
	return contract.ModelFallback(agentName, failedModel, fallbackModel, reason, attempt, maxAttempts)
}

// NewTokenUsageEvent constructs a runtime event.
func NewTokenUsageEvent(sessionID, agentName string, usage *Usage) Event {
	return contract.NewTokenUsageEvent(sessionID, agentName, usage)
}

// SessionUsage constructs a runtime event.
func SessionUsage(sess *session.Session, contextLimit int64, compactionThreshold ...float64) *Usage {
	return contract.SessionUsage(sess, contextLimit, compactionThreshold...)
}

// SessionTitle constructs a runtime event.
func SessionTitle(sessionID, title string) Event { return contract.SessionTitle(sessionID, title) }

// PlanChanged constructs a runtime event.
func PlanChanged(scope, name, action string, version int, agentName string) Event {
	return contract.PlanChanged(scope, name, action, version, agentName)
}

// SessionSummary constructs a runtime event.
func SessionSummary(sessionID, summary, agentName string, firstKeptEntry int, cost float64, model string, usage *chat.Usage) Event {
	return contract.SessionSummary(sessionID, summary, agentName, firstKeptEntry, cost, model, usage)
}

// SessionCompaction constructs a runtime event.
func SessionCompaction(sessionID, status, agentName string) Event {
	return contract.SessionCompaction(sessionID, status, agentName)
}

// SessionCompactionCompleted constructs a runtime event.
func SessionCompactionCompleted(sessionID, outcome, agentName string) Event {
	return contract.SessionCompactionCompleted(sessionID, outcome, agentName)
}

// StreamStopped constructs a runtime event.
func StreamStopped(sessionID, agentName, reason string) Event {
	return contract.StreamStopped(sessionID, agentName, reason)
}

// SessionRecovered constructs a runtime event.
func SessionRecovered(sessionID string) Event { return contract.SessionRecovered(sessionID) }

// Paused constructs a runtime event.
func Paused(sessionID, agentName string) Event { return contract.Paused(sessionID, agentName) }

// ElicitationRequest constructs a runtime event.
func ElicitationRequest(message, mode string, schema any, url, elicitationID, serverElicitationID, sessionID string, meta map[string]any, agentName string) Event {
	return contract.ElicitationRequest(message, mode, schema, url, elicitationID, serverElicitationID, sessionID, meta, agentName)
}

// Authorization constructs a runtime event.
func Authorization(confirmation tools.ElicitationAction, agentName string) Event {
	return contract.Authorization(confirmation, agentName)
}

// MaxIterationsReached constructs a runtime event.
func MaxIterationsReached(maxIterations int) Event {
	return contract.MaxIterationsReached(maxIterations)
}

// MCPInitStarted constructs a runtime event.
func MCPInitStarted(agentName string) Event { return contract.MCPInitStarted(agentName) }

// MCPInitFinished constructs a runtime event.
func MCPInitFinished(agentName string) Event { return contract.MCPInitFinished(agentName) }

// AgentInfo constructs a runtime event.
func AgentInfo(agentName, model, description, welcomeMessage string, contextLimit ...int64) Event {
	return contract.AgentInfo(agentName, model, description, welcomeMessage, contextLimit...)
}

// TeamInfo constructs a runtime event.
func TeamInfo(availableAgents []AgentDetails, currentAgent string) Event {
	return contract.TeamInfo(availableAgents, currentAgent)
}

// AgentSwitching constructs a runtime event.
func AgentSwitching(switching bool, fromAgent, toAgent string) Event {
	return contract.AgentSwitching(switching, fromAgent, toAgent)
}

// ToolsetInfo constructs a runtime event.
func ToolsetInfo(availableTools int, loading bool, agentName string) Event {
	return contract.ToolsetInfo(availableTools, loading, agentName)
}

// RAGIndexingStarted constructs a runtime event.
func RAGIndexingStarted(ragName, strategyName string) Event {
	return contract.RAGIndexingStarted(ragName, strategyName)
}

// RAGIndexingProgress constructs a runtime event.
func RAGIndexingProgress(ragName, strategyName string, current, total int, agentName string) Event {
	return contract.RAGIndexingProgress(ragName, strategyName, current, total, agentName)
}

// RAGIndexingCompleted constructs a runtime event.
func RAGIndexingCompleted(ragName, strategyName string) Event {
	return contract.RAGIndexingCompleted(ragName, strategyName)
}

// HookStarted constructs a runtime event.
func HookStarted(event hooks.EventType, sessionID, agentName string) Event {
	return contract.HookStarted(event, sessionID, agentName)
}

// HookFinished constructs a runtime event.
func HookFinished(event hooks.EventType, sessionID string, result *hooks.Result, dispatchErr error, duration time.Duration, agentName string) Event {
	return contract.HookFinished(event, sessionID, result, dispatchErr, duration, agentName)
}

// HookBlocked constructs a runtime event.
func HookBlocked(toolCall tools.ToolCall, toolDefinition tools.Tool, message, agentName string) Event {
	return contract.HookBlocked(toolCall, toolDefinition, message, agentName)
}

// MessageAdded constructs a runtime event.
func MessageAdded(sessionID string, msg *session.Message, agentName string) Event {
	return contract.MessageAdded(sessionID, msg, agentName)
}

// SubSessionCompleted constructs a runtime event.
func SubSessionCompleted(parentSessionID string, subSession any, agentName string) Event {
	return contract.SubSessionCompleted(parentSessionID, subSession, agentName)
}

func userMessageEvent(msg chat.Message, sessionID string, position int) Event {
	event := UserMessage(msg.Content, sessionID, msg.MultiContent, position).(*UserMessageEvent)
	event.MessageID = msg.MessageID
	return event
}

func BudgetUsage(sessionID, agentName string, snaps []namedBudgetSnapshot) Event {
	budgets := make([]BudgetStatus, 0, len(snaps))
	for _, ns := range snaps {
		s := ns.Snapshot
		var perAgent []AgentBudgetUsage
		if len(s.PerAgent) > 0 {
			perAgent = make([]AgentBudgetUsage, len(s.PerAgent))
			for i, a := range s.PerAgent {
				perAgent[i] = AgentBudgetUsage{
					AgentName:     a.AgentName,
					Cost:          a.Cost,
					Tokens:        a.Tokens,
					ActiveSeconds: a.Active.Seconds(),
				}
			}
		}
		budgets = append(budgets, BudgetStatus{
			Name:           ns.Name,
			Cost:           s.Cost,
			MaxCost:        s.MaxCost,
			Tokens:         s.Tokens,
			MaxTokens:      s.MaxTokens,
			ElapsedSeconds: s.Elapsed.Seconds(),
			MaxTimeSeconds: s.MaxTime.Seconds(),
			Unpriced:       s.Unpriced,
			PerAgent:       perAgent,
		})
	}
	return &BudgetUsageEvent{
		Type:         "budget_usage",
		SessionID:    sessionID,
		AgentContext: newAgentContext(agentName),
		Budgets:      budgets,
	}
}

func BudgetExceeded(sessionID, agentName string, br budgetBreach, stopMessage *chat.Message) Event {
	var stop *BudgetStopMessage
	if stopMessage != nil {
		stop = &BudgetStopMessage{
			AgentName: agentName,
			Message: BudgetStopChatMessage{
				Role:      stopMessage.Role,
				Content:   stopMessage.Content,
				CreatedAt: stopMessage.CreatedAt,
			},
		}
	}
	return &BudgetExceededEvent{
		Type:         "budget_exceeded",
		SessionID:    sessionID,
		AgentContext: newAgentContext(agentName),
		Budget:       br.Budget,
		Limit:        string(br.Limit),
		Used:         br.Used,
		Max:          br.Max,
		ConfigPath:   br.configPath(),
		Message:      br.Message(),
		StopMessage:  stop,
	}
}

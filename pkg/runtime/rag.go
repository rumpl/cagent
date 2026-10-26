package runtime

import (
	"fmt"
	"log/slog"

	ragtypes "github.com/docker/docker-agent/pkg/rag/types"
	"github.com/docker/docker-agent/pkg/runtime/contract"
)

// forwardRAGEvents returns a callback that converts RAG manager events to
// runtime events attributed to the agent named by owner().
func forwardRAGEvents(ragName string, owner func() string, sendEvent func(contract.Event)) ragtypes.EventCallback {
	return func(ragEvent ragtypes.Event) {
		agentName := owner()
		slog.Debug("Forwarding RAG event", "type", ragEvent.Type, "rag", ragName, "agent", agentName)

		switch ragEvent.Type {
		case ragtypes.EventTypeIndexingStarted:
			sendEvent(contract.RAGIndexingStarted(ragName, ragEvent.StrategyName))
		case ragtypes.EventTypeIndexingProgress:
			if ragEvent.Progress != nil {
				sendEvent(contract.RAGIndexingProgress(ragName, ragEvent.StrategyName, ragEvent.Progress.Current, ragEvent.Progress.Total, agentName))
			}
		case ragtypes.EventTypeIndexingComplete:
			sendEvent(contract.RAGIndexingCompleted(ragName, ragEvent.StrategyName))
		case ragtypes.EventTypeUsage:
			sendEvent(contract.NewTokenUsageEvent("", agentName, &contract.Usage{
				InputTokens:   ragEvent.TotalTokens,
				ContextLength: ragEvent.TotalTokens,
				Cost:          ragEvent.Cost,
			}))
		case ragtypes.EventTypeError:
			if ragEvent.Error != nil {
				sendEvent(contract.Error(fmt.Sprintf("RAG %s error: %v", ragName, ragEvent.Error)))
			}
		default:
			slog.Debug("Unhandled RAG event type", "type", ragEvent.Type, "rag", ragName)
		}
	}
}

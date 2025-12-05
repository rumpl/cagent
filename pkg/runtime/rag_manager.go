package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/docker/cagent/pkg/rag"
	ragtypes "github.com/docker/cagent/pkg/rag/types"
	"github.com/docker/cagent/pkg/team"
)

type runtimeRAGManager struct {
	team         *team.Team
	events       EventPublisher
	currentAgent func() string
	initialized  atomic.Bool
}

func newRuntimeRAGManager(t *team.Team, events EventPublisher, currentAgent func() string) *runtimeRAGManager {
	return &runtimeRAGManager{
		team:         t,
		events:       events,
		currentAgent: currentAgent,
	}
}

func (r *runtimeRAGManager) StartBackgroundInit(ctx context.Context) {
	if r.initialized.Swap(true) {
		return
	}

	ragManagers := r.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Starting background RAG initialization with event forwarding", "manager_count", len(ragManagers))

	r.forwardRAGEvents(ctx, ragManagers)

	r.team.InitializeRAG(ctx)
	r.team.StartRAGFileWatchers(ctx)
}

func (r *runtimeRAGManager) Initialize(ctx context.Context) {
	if r.initialized.Swap(true) {
		slog.Debug("RAG already initialized, event forwarding already active", "manager_count", len(r.team.RAGManagers()))
		return
	}

	ragManagers := r.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Setting up RAG initialization (fallback path for non-TUI)", "manager_count", len(ragManagers))

	r.forwardRAGEvents(ctx, ragManagers)

	r.team.InitializeRAG(ctx)
	r.team.StartRAGFileWatchers(ctx)
}

func (r *runtimeRAGManager) IsInitialized() bool {
	return r.initialized.Load()
}

func (r *runtimeRAGManager) forwardRAGEvents(ctx context.Context, ragManagers map[string]*rag.Manager) {
	for _, mgr := range ragManagers {
		go func(mgr *rag.Manager) {
			ragName := mgr.Name()
			slog.Debug("Starting RAG event forwarder goroutine", "rag", ragName)

			for {
				select {
				case <-ctx.Done():
					slog.Debug("RAG event forwarder stopped", "rag", ragName)
					return
				case ragEvent, ok := <-mgr.Events():
					if !ok {
						slog.Debug("RAG events channel closed", "rag", ragName)
						return
					}

					agentName := r.currentAgent()
					slog.Debug("Forwarding RAG event", "type", ragEvent.Type, "rag", ragName, "agent", agentName)

					r.publishRAGEvent(ragName, ragEvent, agentName)
				}
			}
		}(mgr)
	}
}

func (r *runtimeRAGManager) publishRAGEvent(ragName string, ragEvent ragtypes.Event, agentName string) {
	switch ragEvent.Type {
	case ragtypes.EventTypeIndexingStarted:
		r.events.Publish(RAGIndexingStarted(ragName, ragEvent.StrategyName, agentName))

	case ragtypes.EventTypeIndexingProgress:
		if ragEvent.Progress != nil {
			r.events.Publish(RAGIndexingProgress(
				ragName,
				ragEvent.StrategyName,
				ragEvent.Progress.Current,
				ragEvent.Progress.Total,
				agentName,
			))
		}

	case ragtypes.EventTypeIndexingComplete:
		r.events.Publish(RAGIndexingCompleted(ragName, ragEvent.StrategyName, agentName))

	case ragtypes.EventTypeUsage:
		r.events.Publish(TokenUsage(
			"",
			agentName,
			ragEvent.TotalTokens,
			0,
			ragEvent.TotalTokens,
			0,
			ragEvent.Cost,
		))

	case ragtypes.EventTypeError:
		if ragEvent.Error != nil {
			r.events.Publish(Error(fmt.Sprintf("RAG %s error: %v", ragName, ragEvent.Error)))
		}

	default:
		slog.Debug("Unhandled RAG event type", "type", ragEvent.Type, "rag", ragName)
	}
}

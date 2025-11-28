package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/docker/cagent/pkg/rag"
	ragtypes "github.com/docker/cagent/pkg/rag/types"
)

// RAGInitializer is implemented by runtimes that support background RAG initialization.
// Local runtimes use this to start indexing early; remote runtimes typically do not.
type RAGInitializer interface {
	StartBackgroundRAGInit(ctx context.Context, sendEvent func(Event))
}

// RAGTeamProvider defines the interface for accessing team RAG capabilities.
// This allows the RAG service to be decoupled from the full team implementation.
type RAGTeamProvider interface {
	RAGManagers() map[string]*rag.Manager
	InitializeRAG(ctx context.Context)
	StartRAGFileWatchers(ctx context.Context)
}

// RAGService handles RAG initialization and event forwarding for the runtime.
// It encapsulates all RAG-related operations to keep the main runtime focused
// on core agent execution.
type RAGService struct {
	initialized atomic.Bool
	team        RAGTeamProvider
}

// NewRAGService creates a new RAG service instance.
func NewRAGService(team RAGTeamProvider) *RAGService {
	return &RAGService{
		team: team,
	}
}

// StartBackgroundInit initializes RAG in background and forwards events.
// Should be called early (e.g., by App) to start indexing before RunStream.
func (s *RAGService) StartBackgroundInit(ctx context.Context, currentAgent string, sendEvent func(Event)) {
	if s.initialized.Swap(true) {
		return
	}

	ragManagers := s.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Starting background RAG initialization with event forwarding", "manager_count", len(ragManagers))

	// Set up event forwarding BEFORE starting initialization
	// This ensures all events are captured
	s.forwardEvents(ctx, ragManagers, currentAgent, sendEvent)

	// Now start initialization (events will be forwarded)
	s.team.InitializeRAG(ctx)
	s.team.StartRAGFileWatchers(ctx)
}

// Initialize is called within RunStream as a fallback when background init wasn't used
// (e.g., for exec command or API mode where there's no App).
func (s *RAGService) Initialize(ctx context.Context, currentAgent string, events chan Event) {
	// If already initialized via StartBackgroundInit, skip entirely
	// Event forwarding was already set up there
	if s.initialized.Swap(true) {
		slog.Debug("RAG already initialized, event forwarding already active", "manager_count", len(s.team.RAGManagers()))
		return
	}

	ragManagers := s.team.RAGManagers()
	if len(ragManagers) == 0 {
		return
	}

	slog.Debug("Setting up RAG initialization (fallback path for non-TUI)", "manager_count", len(ragManagers))

	// Set up event forwarding BEFORE starting initialization
	s.forwardEvents(ctx, ragManagers, currentAgent, func(event Event) {
		events <- event
	})

	// Start initialization and file watchers
	s.team.InitializeRAG(ctx)
	s.team.StartRAGFileWatchers(ctx)
}

// forwardEvents forwards RAG manager events to the given callback.
// Consolidates duplicated event forwarding logic.
func (s *RAGService) forwardEvents(ctx context.Context, ragManagers map[string]*rag.Manager, currentAgent string, sendEvent func(Event)) {
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

					agentName := currentAgent
					slog.Debug("Forwarding RAG event", "type", ragEvent.Type, "rag", ragName, "agent", agentName)

					switch ragEvent.Type {
					case ragtypes.EventTypeIndexingStarted:
						sendEvent(RAGIndexingStarted(ragName, ragEvent.StrategyName, agentName))
					case ragtypes.EventTypeIndexingProgress:
						if ragEvent.Progress != nil {
							sendEvent(RAGIndexingProgress(ragName, ragEvent.StrategyName, ragEvent.Progress.Current, ragEvent.Progress.Total, agentName))
						}
					case ragtypes.EventTypeIndexingComplete:
						sendEvent(RAGIndexingCompleted(ragName, ragEvent.StrategyName, agentName))
					case ragtypes.EventTypeUsage:
						// Convert RAG usage to TokenUsageEvent so TUI displays it
						sendEvent(TokenUsage(
							"",
							agentName,
							ragEvent.TotalTokens, // input tokens (embeddings)
							0,                    // output tokens (0 for embeddings)
							ragEvent.TotalTokens, // context length
							0,                    // context limit (not applicable)
							ragEvent.Cost,
						))
					case ragtypes.EventTypeError:
						if ragEvent.Error != nil {
							sendEvent(Error(fmt.Sprintf("RAG %s error: %v", ragName, ragEvent.Error)))
						}
					default:
						// Log unhandled events for debugging
						slog.Debug("Unhandled RAG event type", "type", ragEvent.Type, "rag", ragName)
					}
				}
			}
		}(mgr)
	}
}

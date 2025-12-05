package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tools"
	mcptools "github.com/docker/cagent/pkg/tools/mcp"
)

// UnwrapMCPToolset extracts an MCP toolset from a potentially wrapped StartableToolSet.
// Returns the MCP toolset if found, or nil if the toolset is not an MCP toolset.
func UnwrapMCPToolset(toolset tools.ToolSet) *mcptools.Toolset {
	var innerToolset tools.ToolSet
	if startableTS, ok := toolset.(*agent.StartableToolSet); ok {
		innerToolset = startableTS.ToolSet
	} else {
		innerToolset = toolset
	}

	if mcpToolset, ok := innerToolset.(*mcptools.Toolset); ok {
		return mcpToolset
	}

	return nil
}

// ElicitationRequestHandler is a function type for handling elicitation requests
type ElicitationRequestHandler func(ctx context.Context, message string, schema map[string]any) (map[string]any, error)

// Runtime defines the contract for runtime execution
type Runtime interface {
	// CurrentAgentName returns the name of the currently active agent
	CurrentAgentName() string
	// CurrentAgentCommands returns the commands for the active agent
	CurrentAgentCommands(ctx context.Context) map[string]string
	// CurrentWelcomeMessage returns the welcome message for the active agent
	CurrentWelcomeMessage(ctx context.Context) string
	// EmitStartupInfo emits initial agent, team, and toolset information for immediate display
	EmitStartupInfo(ctx context.Context, events chan Event)
	// RunStream starts the agent's interaction loop and returns a channel of events
	RunStream(ctx context.Context, sess *session.Session) <-chan Event
	// Run starts the agent's interaction loop and returns the final messages
	Run(ctx context.Context, sess *session.Session) ([]session.Message, error)
	// Resume allows resuming execution after user confirmation
	Resume(ctx context.Context, confirmationType ResumeType)
	// Summarize generates a summary for the session
	Summarize(ctx context.Context, sess *session.Session, events chan Event)
	// ResumeElicitation sends an elicitation response back to a waiting elicitation request
	ResumeElicitation(_ context.Context, action tools.ElicitationAction, content map[string]any) error
}

type ModelStore interface {
	GetModel(ctx context.Context, modelID string) (*modelsdev.Model, error)
}

// RAGInitializer is implemented by runtimes that support background RAG initialization.
// Local runtimes use this to start indexing early; remote runtimes typically do not.
type RAGInitializer interface {
	StartBackgroundRAGInit(ctx context.Context, sendEvent func(Event))
}

// LocalRuntime manages the execution of agents
type LocalRuntime struct {
	agents             *agentManager
	rootSessionID      string // Root session ID for OAuth state encoding (preserved across sub-sessions)
	tracing            *tracingProvider
	modelsStore        ModelStore
	sessionCompaction  bool
	managedOAuth       bool
	startupInfoEmitted bool // Track if startup info has been emitted to avoid unnecessary duplication
	elicitation        *elicitationHandler
	ragMgr             *runtimeRAGManager
	titleGen           *titleGenerator
	streamProc         *streamProcessor
	toolExec           *toolExecutor
}

type Opt func(*LocalRuntime)

func WithCurrentAgent(agentName string) Opt {
	return func(r *LocalRuntime) {
		_ = r.agents.SetCurrentAgent(agentName)
	}
}

func WithManagedOAuth(managed bool) Opt {
	return func(r *LocalRuntime) {
		r.managedOAuth = managed
	}
}

func WithRootSessionID(sessionID string) Opt {
	return func(r *LocalRuntime) {
		r.rootSessionID = sessionID
	}
}

// WithTracer sets a custom OpenTelemetry tracer; if not provided, tracing is disabled (no-op).
func WithTracer(t trace.Tracer) Opt {
	return func(r *LocalRuntime) {
		r.tracing.SetTracer(t)
	}
}

func WithSessionCompaction(sessionCompaction bool) Opt {
	return func(r *LocalRuntime) {
		r.sessionCompaction = sessionCompaction
	}
}

func WithModelStore(store ModelStore) Opt {
	return func(r *LocalRuntime) {
		r.modelsStore = store
	}
}

// New creates a new runtime for an agent and its team
func New(agents *team.Team, opts ...Opt) (*LocalRuntime, error) {
	modelsStore, err := modelsdev.NewStore()
	if err != nil {
		return nil, err
	}

	tracing := newTracingProvider(nil)

	r := &LocalRuntime{
		agents:            newAgentManager(agents, &channelPublisher{}, tracing),
		tracing:           tracing,
		modelsStore:       modelsStore,
		sessionCompaction: true,
		managedOAuth:      true,
	}

	for _, opt := range opts {
		opt(r)
	}

	r.elicitation = newElicitationHandler(
		&channelPublisher{},
		func() string { return r.agents.CurrentAgentName() },
	)

	r.ragMgr = newRuntimeRAGManager(
		agents,
		&channelPublisher{},
		func() string { return r.agents.CurrentAgentName() },
	)

	r.streamProc = newStreamProcessor(&channelPublisher{})

	r.toolExec = newToolExecutor(&channelPublisher{}, tracing)

	// Validate that we have at least one agent and that the current agent exists
	if _, err = r.agents.Agent(r.agents.CurrentAgentName()); err != nil {
		return nil, err
	}

	// Check if a requested agent was set via WithCurrentAgent but not found
	if err := r.agents.ValidateRequestedAgent(); err != nil {
		return nil, err
	}

	slog.Debug("Creating new runtime", "agent", r.agents.CurrentAgentName(), "available_agents", agents.Size())

	return r, nil
}

// StartBackgroundRAGInit initializes RAG in background and forwards events
// Should be called early (e.g., by App) to start indexing before RunStream
func (r *LocalRuntime) StartBackgroundRAGInit(ctx context.Context, sendEvent func(Event)) {
	r.ragMgr.events = &callbackPublisher{callback: sendEvent}
	r.ragMgr.StartBackgroundInit(ctx)
}

// InitializeRAG is called within RunStream as a fallback when background init wasn't used
// (e.g., for exec command or API mode where there's no App)
func (r *LocalRuntime) InitializeRAG(ctx context.Context, events chan Event) {
	r.ragMgr.events = &channelPublisher{ch: events}
	r.ragMgr.Initialize(ctx)
}

func (r *LocalRuntime) CurrentAgentName() string {
	return r.agents.CurrentAgentName()
}

func (r *LocalRuntime) CurrentAgentCommands(ctx context.Context) map[string]string {
	return r.agents.CurrentAgentCommands(ctx)
}

func (r *LocalRuntime) CurrentWelcomeMessage(ctx context.Context) string {
	return r.agents.CurrentWelcomeMessage(ctx)
}

// CurrentMCPPrompts returns the available MCP prompts from all active MCP toolsets
// for the current agent. It discovers prompts by calling ListPrompts on each MCP toolset
// and aggregates the results into a map keyed by prompt name.
func (r *LocalRuntime) CurrentMCPPrompts(ctx context.Context) map[string]mcptools.PromptInfo {
	prompts := make(map[string]mcptools.PromptInfo)

	// Get the current agent to access its toolsets
	currentAgent := r.CurrentAgent()
	if currentAgent == nil {
		slog.Warn("No current agent available for MCP prompt discovery")
		return prompts
	}

	// Iterate through all toolsets of the current agent
	for _, toolset := range currentAgent.ToolSets() {
		if mcpToolset := UnwrapMCPToolset(toolset); mcpToolset != nil {
			slog.Debug("Found MCP toolset", "toolset", mcpToolset)
			// Discover prompts from this MCP toolset
			mcpPrompts := r.discoverMCPPrompts(ctx, mcpToolset)

			// Merge prompts into the result map
			// If there are name conflicts, the later toolset's prompt will override
			for name, promptInfo := range mcpPrompts {
				prompts[name] = promptInfo
			}
		} else {
			slog.Debug("Toolset is not an MCP toolset", "type", fmt.Sprintf("%T", toolset))
		}
	}

	slog.Debug("Discovered MCP prompts", "agent", currentAgent.Name(), "prompt_count", len(prompts))
	return prompts
}

// discoverMCPPrompts queries an MCP toolset for available prompts and converts them
// to PromptInfo structures. This method handles the MCP protocol communication
// and gracefully handles any errors during prompt discovery.
func (r *LocalRuntime) discoverMCPPrompts(ctx context.Context, toolset *mcptools.Toolset) map[string]mcptools.PromptInfo {
	prompts := make(map[string]mcptools.PromptInfo)

	// Check if the toolset is started (required for MCP operations)
	// Note: We need to implement IsStarted() method on the MCP Toolset if it doesn't exist
	// For now, we'll proceed and handle any errors from ListPrompts

	// Call ListPrompts on the MCP toolset
	// Note: We need to implement this method on the Toolset to expose MCP prompt functionality
	mcpPrompts, err := toolset.ListPrompts(ctx)
	if err != nil {
		slog.Warn("Failed to list MCP prompts from toolset", "error", err)
		return prompts
	}

	// Convert MCP prompts to our internal format
	for _, mcpPrompt := range mcpPrompts {
		promptInfo := mcptools.PromptInfo{
			Name:        mcpPrompt.Name,
			Description: mcpPrompt.Description,
			Arguments:   make([]mcptools.PromptArgument, 0),
		}

		// Convert MCP prompt arguments if they exist
		if mcpPrompt.Arguments != nil {
			for _, arg := range mcpPrompt.Arguments {
				promptArg := mcptools.PromptArgument{
					Name:        arg.Name,
					Description: arg.Description,
					Required:    arg.Required,
				}
				promptInfo.Arguments = append(promptInfo.Arguments, promptArg)
			}
		}

		prompts[mcpPrompt.Name] = promptInfo
		slog.Debug("Discovered MCP prompt", "name", mcpPrompt.Name, "args_count", len(promptInfo.Arguments))
	}

	return prompts
}

// CurrentAgent returns the current agent
func (r *LocalRuntime) CurrentAgent() *agent.Agent {
	return r.agents.CurrentAgent()
}

// EmitStartupInfo emits initial agent, team, and toolset information for immediate sidebar display
func (r *LocalRuntime) EmitStartupInfo(ctx context.Context, events chan Event) {
	// Prevent duplicate emissions
	if r.startupInfoEmitted {
		return
	}

	a := r.CurrentAgent()

	// Emit agent information for sidebar display
	var modelID string
	if model := a.Model(); model != nil {
		modelID = model.ID()
	}
	events <- AgentInfo(a.Name(), modelID, a.Description())

	// Emit team information
	availableAgents := r.agents.AgentNames()
	events <- TeamInfo(availableAgents, r.agents.CurrentAgentName())

	// Emit agent warnings (if any)
	r.agents.events = &channelPublisher{ch: events}
	r.agents.EmitAgentWarnings(a)

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.Warn("Failed to get agent tools during startup", "agent", a.Name(), "error", err)
		// Emit toolset info with 0 tools if we can't get them
		events <- ToolsetInfo(0, r.agents.CurrentAgentName())
		r.startupInfoEmitted = true
		return
	}

	// Emit toolset information
	events <- ToolsetInfo(len(agentTools), r.agents.CurrentAgentName())
	r.startupInfoEmitted = true
}

// registerDefaultTools registers the default tool handlers
func (r *LocalRuntime) registerDefaultTools() {
	slog.Debug("Registering default tools")
	r.registerTaskTransferHandlers()
}

func (r *LocalRuntime) finalizeEventChannel(ctx context.Context, sess *session.Session, events chan Event) {
	defer close(events)

	events <- StreamStopped(sess.ID, r.agents.CurrentAgentName())

	telemetry.RecordSessionEnd(ctx)

	// Wait for title generation if it's in progress
	r.titleGen.Wait()
}

// RunStream starts the agent's interaction loop and returns a channel of events
func (r *LocalRuntime) RunStream(ctx context.Context, sess *session.Session) <-chan Event {
	slog.Debug("Starting runtime stream", "agent", r.agents.CurrentAgentName(), "session_id", sess.ID)
	events := make(chan Event, 128)

	go func() {
		telemetry.RecordSessionStart(ctx, r.agents.CurrentAgentName(), sess.ID)

		ctx, sessionSpan := r.tracing.StartSpan(ctx, "runtime.session", trace.WithAttributes(
			attribute.String("agent", r.agents.CurrentAgentName()),
			attribute.String("session.id", sess.ID),
		))
		defer sessionSpan.End()

		// Set up elicitation handler with the events channel
		r.elicitation.events = &channelPublisher{ch: events}
		r.agents.events = &channelPublisher{ch: events}

		// Set elicitation handler on all MCP toolsets before getting tools
		a := r.CurrentAgent()

		// Emit agent information for sidebar display
		var modelID string
		if model := a.Model(); model != nil {
			modelID = model.ID()
		}
		events <- AgentInfo(a.Name(), modelID, a.Description())

		// Emit team information
		availableAgents := r.agents.AgentNames()
		events <- TeamInfo(availableAgents, r.agents.CurrentAgentName())

		// Initialize RAG and forward events
		r.InitializeRAG(ctx, events)

		r.agents.EmitAgentWarnings(a)

		for _, toolset := range a.ToolSets() {
			toolset.SetElicitationHandler(r.elicitation.GetHandlerFunc())
			toolset.SetOAuthSuccessHandler(func() {
				events <- Authorization(tools.ElicitationActionAccept, r.agents.CurrentAgentName())
			})
			toolset.SetManagedOAuth(r.managedOAuth)
		}

		agentTools, err := r.agents.GetTools(ctx, a)
		if err != nil {
			events <- Error(fmt.Sprintf("failed to get tools: %v", err))
			return
		}

		// Emit toolset information
		events <- ToolsetInfo(len(agentTools), r.agents.CurrentAgentName())

		messages := sess.GetMessages(a)
		if sess.SendUserMessage {
			events <- UserMessage(messages[len(messages)-1].Content)
		}

		events <- StreamStarted(sess.ID, a.Name())

		defer r.finalizeEventChannel(ctx, sess, events)

		r.registerDefaultTools()

		r.titleGen = newTitleGenerator(
			&channelPublisher{ch: events},
			func() provider.Provider { return r.CurrentAgent().Model() },
			func() string { return r.agents.CurrentAgentName() },
		)

		if sess.Title == "" {
			r.titleGen.Generate(ctx, sess)
		}

		iteration := 0
		// Use a runtime copy of maxIterations so we don't modify the session's persistent config
		runtimeMaxIterations := sess.MaxIterations

		for {
			// Set elicitation handler on all MCP toolsets before getting tools
			a := r.CurrentAgent()

			r.agents.EmitAgentWarnings(a)

			for _, toolset := range a.ToolSets() {
				toolset.SetElicitationHandler(r.elicitation.GetHandlerFunc())
				toolset.SetOAuthSuccessHandler(func() {
					events <- Authorization("confirmed", r.agents.CurrentAgentName())
				})
			}

			agentTools, err := r.agents.GetTools(ctx, a)
			if err != nil {
				events <- Error(fmt.Sprintf("failed to get tools: %v", err))
				return
			}

			// Check iteration limit
			if runtimeMaxIterations > 0 && iteration >= runtimeMaxIterations {
				slog.Debug("Maximum iterations reached", "agent", a.Name(), "iterations", iteration, "max", runtimeMaxIterations)
				events <- MaxIterationsReached(runtimeMaxIterations)

				// Wait for user decision
				select {
				case resumeType := <-r.toolExec.resumeChan:
					if resumeType == ResumeTypeApprove {
						slog.Debug("User chose to continue after max iterations", "agent", a.Name())
						runtimeMaxIterations = iteration + 10
					} else {
						slog.Debug("User chose to exit after max iterations", "agent", a.Name())
						// Synthesize a final assistant message so callers (e.g., parent agents)
						// receive a non-empty response and providers are not given empty tool outputs.
						assistantMessage := chat.Message{
							Role:      chat.MessageRoleAssistant,
							Content:   fmt.Sprintf("I have reached the maximum number of iterations (%d). Stopping as requested by user.", runtimeMaxIterations),
							CreatedAt: time.Now().Format(time.RFC3339),
						}
						sess.AddMessage(session.NewAgentMessage(a, &assistantMessage))
						return
					}
				case <-ctx.Done():
					slog.Debug("Context cancelled while waiting for max iterations decision", "agent", a.Name())
					return
				}
			}
			iteration++
			// Exit immediately if the stream context has been cancelled (e.g., Ctrl+C)
			if err := ctx.Err(); err != nil {
				slog.Debug("Runtime stream context cancelled, stopping loop", "agent", a.Name(), "session_id", sess.ID)
				return
			}
			slog.Debug("Starting conversation loop iteration", "agent", a.Name())

			streamCtx, streamSpan := r.tracing.StartSpan(ctx, "runtime.stream", trace.WithAttributes(
				attribute.String("agent", a.Name()),
				attribute.String("session.id", sess.ID),
			))

			model := a.Model()
			modelID := model.ID()
			slog.Debug("Using agent", "agent", a.Name(), "model", modelID)
			slog.Debug("Getting model definition", "model_id", modelID)
			m, err := r.modelsStore.GetModel(ctx, modelID)
			if err != nil {
				slog.Debug("Failed to get model definition", "error", err)
			}

			var contextLimit int64
			if m != nil {
				contextLimit = int64(m.Limit.Context)
			}

			if m != nil && r.sessionCompaction {
				if sess.InputTokens+sess.OutputTokens > int64(float64(contextLimit)*0.9) {
					r.Summarize(ctx, sess, events)
					events <- TokenUsage(sess.ID, r.agents.CurrentAgentName(), sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)
				}
			}

			messages := sess.GetMessages(a)
			slog.Debug("Retrieved messages for processing", "agent", a.Name(), "message_count", len(messages))

			slog.Debug("Creating chat completion stream", "agent", a.Name())
			stream, err := model.CreateChatCompletionStream(streamCtx, messages, agentTools)
			if err != nil {
				streamSpan.RecordError(err)
				streamSpan.SetStatus(codes.Error, "creating chat completion")
				slog.Error("Failed to create chat completion stream", "agent", a.Name(), "error", err)
				// Track error in telemetry
				telemetry.RecordError(ctx, err.Error())
				events <- Error(fmt.Sprintf("creating chat completion: %v", err))
				streamSpan.End()
				return
			}

			slog.Debug("Processing stream", "agent", a.Name())
			r.streamProc.events = &channelPublisher{ch: events}
			res, err := r.streamProc.ProcessStream(ctx, stream, a, agentTools, sess, m)
			if err != nil {
				// Treat context cancellation as a graceful stop
				if errors.Is(err, context.Canceled) {
					slog.Debug("Model stream canceled by context", "agent", a.Name(), "session_id", sess.ID)
					streamSpan.End()
					return
				}
				streamSpan.RecordError(err)
				streamSpan.SetStatus(codes.Error, "error handling stream")
				slog.Error("Error handling stream", "agent", a.Name(), "error", err)
				// Track error in telemetry
				telemetry.RecordError(ctx, err.Error())
				events <- Error(err.Error())
				streamSpan.End()
				return
			}
			streamSpan.SetAttributes(
				attribute.Int("tool.calls", len(res.Calls)),
				attribute.Int("content.length", len(res.Content)),
				attribute.Bool("stopped", res.Stopped),
			)
			streamSpan.End()
			slog.Debug("Stream processed", "agent", a.Name(), "tool_calls", len(res.Calls), "content_length", len(res.Content), "stopped", res.Stopped)

			// Add assistant message to conversation history, but skip empty assistant messages
			// Providers reject assistant messages that have neither content nor tool calls.
			if strings.TrimSpace(res.Content) != "" || len(res.Calls) > 0 {
				assistantMessage := chat.Message{
					Role:              chat.MessageRoleAssistant,
					Content:           res.Content,
					ReasoningContent:  res.ReasoningContent,
					ThinkingSignature: res.ThinkingSignature,
					ThoughtSignature:  res.ThoughtSignature,
					ToolCalls:         res.Calls,
					CreatedAt:         time.Now().Format(time.RFC3339),
				}

				sess.AddMessage(session.NewAgentMessage(a, &assistantMessage))
				slog.Debug("Added assistant message to session", "agent", a.Name(), "total_messages", len(sess.GetAllMessages()))
			} else {
				slog.Debug("Skipping empty assistant message (no content and no tool calls)", "agent", a.Name())
			}

			events <- TokenUsage(sess.ID, r.agents.CurrentAgentName(), sess.InputTokens, sess.OutputTokens, sess.InputTokens+sess.OutputTokens, contextLimit, sess.Cost)

			r.toolExec.events = &channelPublisher{ch: events}
			r.toolExec.ProcessToolCalls(ctx, sess, res.Calls, agentTools, a, events)

			if res.Stopped {
				slog.Debug("Conversation stopped", "agent", a.Name())
				break
			}
		}
	}()

	return events
}

func (r *LocalRuntime) Resume(ctx context.Context, confirmationType ResumeType) {
	r.toolExec.Resume(ctx, confirmationType)
}

// ResumeElicitation sends an elicitation response back to a waiting elicitation request
func (r *LocalRuntime) ResumeElicitation(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
	return r.elicitation.Resume(ctx, action, content)
}

// Run starts the agent's interaction loop
func (r *LocalRuntime) Run(ctx context.Context, sess *session.Session) ([]session.Message, error) {
	eventsChan := r.RunStream(ctx, sess)

	for event := range eventsChan {
		if errEvent, ok := event.(*ErrorEvent); ok {
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	return sess.GetAllMessages(), nil
}

// Summarize generates a summary for the session based on the conversation history
func (r *LocalRuntime) Summarize(ctx context.Context, sess *session.Session, events chan Event) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", r.agents.CurrentAgentName())
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", r.agents.CurrentAgentName())
	}()

	// Create conversation history for summarization
	var conversationHistory strings.Builder
	messages := sess.GetAllMessages()

	// Check if session is empty
	if len(messages) == 0 {
		events <- &WarningEvent{Message: "Session is empty. Start a conversation before compacting."}
		return
	}
	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			continue // Skip system messages for summarization
		}
		conversationHistory.WriteString(fmt.Sprintf("\n%s: %s", role, messages[i].Message.Content))
	}

	// Create a new session for summary generation
	systemPrompt := "You are a helpful AI assistant that creates comprehensive summaries of conversations. You will be given a conversation history and asked to create a concise yet thorough summary that captures the key points, decisions made, and outcomes."
	userPrompt := fmt.Sprintf("Based on the following conversation between a user and an AI assistant, create a comprehensive summary that captures:\n- The main topics discussed\n- Key information exchanged\n- Decisions made or conclusions reached\n- Important outcomes or results\n\nProvide a well-structured summary (2-4 paragraphs) that someone could read to understand what happened in this conversation. Return ONLY the summary text, nothing else.\n\nConversation history:%s\n\nGenerate a summary for this conversation:", conversationHistory.String())
	newModel := provider.CloneWithOptions(ctx, r.CurrentAgent().Model(), options.WithStructuredOutput(nil))
	newTeam := team.New(
		team.WithAgents(agent.New("root", systemPrompt, agent.WithModel(newModel))),
	)

	summarySession := session.New(session.WithSystemMessage(systemPrompt))
	summarySession.AddMessage(session.UserMessage(userPrompt))
	summarySession.Title = "Generating summary..."

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create summary generator runtime", "error", err)
		return
	}

	// Run the summary generation
	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		slog.Error("Failed to generate session summary", "session_id", sess.ID, "error", err)
		return
	}

	summary := summarySession.GetLastAssistantMessageContent()
	if summary == "" {
		return
	}
	// Add the summary to the session as a summary item
	sess.Messages = append(sess.Messages, session.Item{Summary: summary})
	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, r.agents.CurrentAgentName())
}

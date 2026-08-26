package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// ErrRemoteSessionBusy reports that the server refused to start a turn
// because the session is already running one — typically another client
// attached to the same session got there first.
var ErrRemoteSessionBusy = errors.New("remote session is already processing a turn")

// Client is an HTTP client for the docker agent server API
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	authToken  string
	registry   map[string]func() Event
}

// ClientOption is a function for configuring the Client
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithAuthToken sets the bearer token for authentication
func WithAuthToken(token string) ClientOption {
	return func(c *Client) {
		c.authToken = token
	}
}

// WithTimeout sets the HTTP client timeout (deprecated: prefer per-request timeouts)
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{}
		}
		c.httpClient.Timeout = timeout
	}
}

// timeoutFor returns the appropriate timeout for a request category
func (c *Client) timeoutFor(category string) time.Duration {
	// Short timeout for metadata/CRUD operations
	if category == "metadata" || category == "crud" {
		return 30 * time.Second
	}
	// Long timeout for streaming/SSE operations
	return 5 * time.Minute
}

// NewClient creates a new HTTP client for the docker agent server
func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	client := &Client{
		baseURL: parsedURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		registry: map[string]func() Event{
			"user_message":           func() Event { return &UserMessageEvent{} },
			"tool_call":              func() Event { return &ToolCallEvent{} },
			"tool_call_output":       func() Event { return &ToolCallOutputEvent{} },
			"tool_call_response":     func() Event { return &ToolCallResponseEvent{} },
			"tool_call_confirmation": func() Event { return &ToolCallConfirmationEvent{} },
			"token_usage":            func() Event { return &TokenUsageEvent{} },
			"stream_stopped":         func() Event { return &StreamStoppedEvent{} },
			"runtime_paused":         func() Event { return &PausedEvent{} },
			"stream_started":         func() Event { return &StreamStartedEvent{} },
			"shell":                  func() Event { return &ShellOutputEvent{} },
			"session_title":          func() Event { return &SessionTitleEvent{} },
			"session_plan_updated":   func() Event { return &SessionPlanUpdatedEvent{} },
			"plan_changed":           func() Event { return &PlanChangedEvent{} },
			"session_summary":        func() Event { return &SessionSummaryEvent{} },
			"session_compaction":     func() Event { return &SessionCompactionEvent{} },
			"partial_tool_call":      func() Event { return &PartialToolCallEvent{} },
			"max_iterations_reached": func() Event { return &MaxIterationsReachedEvent{} },
			"budget_usage":           func() Event { return &BudgetUsageEvent{} },
			"budget_exceeded":        func() Event { return &BudgetExceededEvent{} },
			"error":                  func() Event { return &ErrorEvent{} },
			"elicitation_request":    func() Event { return &ElicitationRequestEvent{} },
			"authorization_event":    func() Event { return &AuthorizationEvent{} },
			"agent_choice":           func() Event { return &AgentChoiceEvent{} },
			"agent_choice_reasoning": func() Event { return &AgentChoiceReasoningEvent{} },
			"mcp_init_started":       func() Event { return &MCPInitStartedEvent{} },
			"mcp_init_finished":      func() Event { return &MCPInitFinishedEvent{} },
			"agent_info":             func() Event { return &AgentInfoEvent{} },
			"team_info":              func() Event { return &TeamInfoEvent{} },
			"toolset_info":           func() Event { return &ToolsetInfoEvent{} },
			"agent_switching":        func() Event { return &AgentSwitchingEvent{} },
			"warning":                func() Event { return &WarningEvent{} },
			"hook_blocked":           func() Event { return &HookBlockedEvent{} },
			"hook_started":           func() Event { return &HookStartedEvent{} },
			"hook_finished":          func() Event { return &HookFinishedEvent{} },
			"rag_indexing_started":   func() Event { return &RAGIndexingStartedEvent{} },
			"rag_indexing_progress":  func() Event { return &RAGIndexingProgressEvent{} },
			"rag_indexing_completed": func() Event { return &RAGIndexingCompletedEvent{} },
			"message_added":          func() Event { return &MessageAddedEvent{} },
			"model_fallback":         func() Event { return &ModelFallbackEvent{} },
			"sub_session_completed":  func() Event { return &SubSessionCompletedEvent{} },
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error string `json:"error"`
}

// doRequest performs an HTTP request and handles common response patterns
func (c *Client) doRequest(ctx context.Context, method, endpoint string, body, result any) error {
	return c.doRequestWithTimeout(ctx, method, endpoint, body, result, "crud")
}

// doRequestWithTimeout performs an HTTP request with explicit timeout category
func (c *Client) doRequestWithTimeout(ctx context.Context, method, endpoint string, body, result any, timeoutCategory string) error {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	u := *c.baseURL
	u.Path = path.Join(u.Path, endpoint)

	// Apply per-request timeout based on category
	timeout := c.timeoutFor(timeoutCategory)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
	}

	return nil
}

// GetAgents retrieves all available agents
func (c *Client) GetAgents(ctx context.Context) ([]api.Agent, error) {
	var agents []api.Agent
	err := c.doRequest(ctx, http.MethodGet, "/api/agents", nil, &agents)
	return agents, err
}

// GetAgent retrieves an agent by ID
func (c *Client) GetAgent(ctx context.Context, id string) (*latest.Config, error) {
	var config latest.Config
	err := c.doRequest(ctx, http.MethodGet, "/api/agents/"+id, nil, &config)
	return &config, err
}

// CreateAgent creates a new agent using a prompt
func (c *Client) CreateAgent(ctx context.Context, prompt string) (*api.CreateAgentResponse, error) {
	req := api.CreateAgentRequest{Prompt: prompt}
	var resp api.CreateAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents", req, &resp)
	return &resp, err
}

// CreateAgentConfig creates a new agent manually with YAML configuration
func (c *Client) CreateAgentConfig(ctx context.Context, filename, model, description, instruction string) (*api.CreateAgentConfigResponse, error) {
	req := api.CreateAgentConfigRequest{
		Filename:    filename,
		Model:       model,
		Description: description,
		Instruction: instruction,
	}
	var resp api.CreateAgentConfigResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/config", req, &resp)
	return &resp, err
}

// EditAgentConfig edits an agent configuration
func (c *Client) EditAgentConfig(ctx context.Context, filename string, config latest.Config) (*api.EditAgentConfigResponse, error) {
	req := api.EditAgentConfigRequest{
		AgentConfig: config,
		Filename:    filename,
	}
	var resp api.EditAgentConfigResponse
	err := c.doRequest(ctx, "PUT", "/api/agents/config", req, &resp)
	return &resp, err
}

// ImportAgent imports an agent from a file path
func (c *Client) ImportAgent(ctx context.Context, filePath string) (*api.ImportAgentResponse, error) {
	req := api.ImportAgentRequest{FilePath: filePath}
	var resp api.ImportAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/import", req, &resp)
	return &resp, err
}

// ExportAgents exports multiple agents as a zip file
func (c *Client) ExportAgents(ctx context.Context) (*api.ExportAgentsResponse, error) {
	var resp api.ExportAgentsResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/export", nil, &resp)
	return &resp, err
}

// PullAgent pulls an agent from a remote registry
func (c *Client) PullAgent(ctx context.Context, name string) (*api.PullAgentResponse, error) {
	req := api.PullAgentRequest{Name: name}
	var resp api.PullAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/pull", req, &resp)
	return &resp, err
}

// PushAgent pushes an agent to a remote registry
func (c *Client) PushAgent(ctx context.Context, filepath, tag string) (*api.PushAgentResponse, error) {
	req := api.PushAgentRequest{Filepath: filepath, Tag: tag}
	var resp api.PushAgentResponse
	err := c.doRequest(ctx, http.MethodPost, "/api/agents/push", req, &resp)
	return &resp, err
}

// DeleteAgent deletes an agent by file path
func (c *Client) DeleteAgent(ctx context.Context, filePath string) (*api.DeleteAgentResponse, error) {
	req := api.DeleteAgentRequest{FilePath: filePath}
	var resp api.DeleteAgentResponse
	err := c.doRequest(ctx, "DELETE", "/api/agents", req, &resp)
	return &resp, err
}

// GetSessions retrieves all sessions
func (c *Client) GetSessions(ctx context.Context) ([]api.SessionsResponse, error) {
	var sessions []api.SessionsResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/sessions", nil, &sessions)
	return sessions, err
}

// GetSession retrieves a session by ID
func (c *Client) GetSession(ctx context.Context, id string) (*api.SessionResponse, error) {
	var sess api.SessionResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/sessions/"+id, nil, &sess)
	return &sess, err
}

// GetSessionSnapshot returns a session's full state in one call: its stored
// messages plus the sequence number they correspond to on the event stream.
// Reading it and then tailing [Client.StreamSessionEventsFrom] from
// LastEventSeq rebuilds the session without missing anything in between.
func (c *Client) GetSessionSnapshot(ctx context.Context, id string) (*api.SessionSnapshotResponse, error) {
	var snapshot api.SessionSnapshotResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/sessions/"+id+"/snapshot", nil, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// CreateSession creates a new session
func (c *Client) CreateSession(ctx context.Context, sessTemplate *session.Session) (*session.Session, error) {
	var sess session.Session
	err := c.doRequest(ctx, http.MethodPost, "/api/sessions", sessTemplate, &sess)
	return &sess, err
}

// ResumeSession resumes a session by ID with optional rejection reason or tool name
func (c *Client) ResumeSession(ctx context.Context, id, confirmation, reason, toolName string) error {
	req := api.ResumeSessionRequest{Confirmation: confirmation, Reason: reason, ToolName: toolName}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+id+"/resume", req, nil)
}

// SteerSession injects user messages into a running session mid-turn.
func (c *Client) SteerSession(ctx context.Context, sessionID string, messages []api.Message) error {
	req := api.SteerSessionRequest{Messages: messages}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+sessionID+"/steer", req, nil)
}

// FollowUpSession queues messages for end-of-turn processing.
func (c *Client) FollowUpSession(ctx context.Context, sessionID string, messages []api.Message) error {
	req := api.SteerSessionRequest{Messages: messages}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+sessionID+"/followup", req, nil)
}

// DeleteSession deletes a session by ID
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.doRequest(ctx, "DELETE", "/api/sessions/"+id, nil, nil)
}

// GetDesktopToken retrieves a desktop authentication token
func (c *Client) GetDesktopToken(ctx context.Context) (*api.DesktopTokenResponse, error) {
	var resp api.DesktopTokenResponse
	err := c.doRequest(ctx, http.MethodGet, "/api/desktop/token", nil, &resp)
	return &resp, err
}

// RunAgent executes an agent and returns a channel of streaming frames: the
// turn's events, each tagged with its position in the session's event stream
// so a caller tailing that stream too can recognise them (see
// [SessionStreamFrame]). The optional model override is persisted on the
// session's current agent before the user messages are appended; pass an
// empty string to leave the existing override (if any) untouched.
func (c *Client) RunAgent(ctx context.Context, sessionID, agent string, messages []api.Message, model string) (<-chan SessionStreamFrame, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, "", messages, model)
}

// RunAgentWithAgentName executes an agent with a specific agent name. See
// [Client.RunAgent] for what comes back and for the semantics of model.
func (c *Client) RunAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message, model string) (<-chan SessionStreamFrame, error) {
	return c.runAgentWithAgentName(ctx, sessionID, agent, agentName, messages, model)
}

func (c *Client) runAgentWithAgentName(ctx context.Context, sessionID, agent, agentName string, messages []api.Message, model string) (<-chan SessionStreamFrame, error) {
	endpoint := "/api/sessions/" + sessionID + "/agent/" + agent
	if agentName != "" {
		endpoint += "/" + agentName
	}

	jsonBody, err := json.Marshal(api.RunAgentRequest{Messages: messages, Model: model})
	if err != nil {
		return nil, fmt.Errorf("marshaling messages: %w", err)
	}

	u := *c.baseURL
	u.Path = path.Join(u.Path, endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading error response body: %w", err)
		}

		var errResp ErrorResponse
		detail := string(respBody)
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			detail = errResp.Error
		}
		if resp.StatusCode == http.StatusConflict {
			// Another client is running a turn on this session right now.
			// Typed so the caller can react instead of parsing the message.
			return nil, fmt.Errorf("%w: %s", ErrRemoteSessionBusy, detail)
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, detail)
	}

	frames := make(chan SessionStreamFrame, defaultEventChannelCapacity)
	go c.readSSEFrames(ctx, resp.Body, frames)
	return frames, nil
}

// readSSEFrames decodes an SSE body into frames until it ends, then closes
// out. It serves both event streams the server exposes — a turn's own
// response and the session-scoped stream — which carry the same framing:
// an optional "id: <seq>" line followed by a "data: <event>" line.
//
// done, when non-nil, reports whether the stream was shut down deliberately;
// a read error is then not surfaced as an event.
func (c *Client) readSSEFrames(ctx context.Context, body io.ReadCloser, out chan<- SessionStreamFrame, done ...func() bool) {
	defer close(out)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	// A single SSE line can carry a large tool response; raise the cap
	// above bufio's 64 KiB default so an oversized line does not silently
	// truncate the stream (bufio.ErrTooLong).
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxSSELineBytes)

	var seq uint64
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] == ':' {
			continue
		}

		if rawSeq, ok := bytes.CutPrefix(line, []byte("id: ")); ok {
			seq, _ = strconv.ParseUint(string(rawSeq), 10, 64)
			continue
		}

		data, ok := bytes.CutPrefix(line, []byte("data: "))
		if !ok {
			continue
		}
		// An event's id line precedes its data line; anything without one
		// is outside the sequenced stream (a control frame, or a server
		// that does not sequence this stream).
		frameSeq := seq
		seq = 0

		slog.DebugContext(ctx, "received event", "data", string(data))

		var baseEvent struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &baseEvent); err != nil {
			slog.DebugContext(ctx, "failed to unmarshal event type", "error", err)
			continue
		}

		switch baseEvent.Type {
		case SessionStreamGap:
			out <- SessionStreamFrame{Control: SessionStreamGap}
			continue
		case SessionStreamExited:
			out <- SessionStreamFrame{Seq: frameSeq, Control: SessionStreamExited}
			continue
		}

		createEvent, found := c.registry[baseEvent.Type]
		if !found {
			slog.DebugContext(ctx, "unknown event type", "type", baseEvent.Type)
			continue
		}

		event := createEvent()
		if err := json.Unmarshal(data, &event); err != nil {
			slog.DebugContext(ctx, "failed to unmarshal event", "error", err)
			continue
		}

		out <- SessionStreamFrame{Seq: frameSeq, Event: event}
	}

	// Surface a read failure (e.g. an over-long line) instead of ending the
	// stream silently — otherwise the run appears to stop with no error after
	// the last event that fit. A deliberate shutdown is not a failure.
	if err := scanner.Err(); err != nil {
		if len(done) > 0 && done[0]() {
			return
		}
		slog.DebugContext(ctx, "scanner error", "error", err)
		out <- SessionStreamFrame{Event: Error(fmt.Sprintf("reading event stream: %v", err))}
	}
}

// GetAllSessions retrieves all sessions from the remote store.
func (c *Client) GetAllSessions(ctx context.Context) ([]session.Session, error) {
	var sessions []session.Session
	err := c.doRequest(ctx, http.MethodGet, "/api/sessions", nil, &sessions)
	return sessions, err
}

// DeleteRemoteSession deletes a session from the remote store.
func (c *Client) DeleteRemoteSession(ctx context.Context, sessionID string) error {
	return c.doRequest(ctx, http.MethodDelete, "/api/sessions/"+sessionID, nil, nil)
}

func (c *Client) ResumeElicitation(ctx context.Context, sessionID string, action tools.ElicitationAction, content map[string]any, elicitationID ...string) error {
	req := api.ResumeElicitationRequest{Action: string(action), Content: content, ElicitationID: firstElicitationID(elicitationID)}
	return c.doRequest(ctx, http.MethodPost, "/api/sessions/"+sessionID+"/elicitation", req, nil)
}

// UpdateSessionTitle updates the title of a session
func (c *Client) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	req := api.UpdateSessionTitleRequest{Title: title}
	return c.doRequest(ctx, http.MethodPatch, "/api/sessions/"+sessionID+"/title", req, nil)
}

// GetAgentToolCount returns the number of tools available for an agent.
func (c *Client) GetAgentToolCount(ctx context.Context, agentFilename, agentName string) (int, error) {
	var resp struct {
		AvailableTools int `json:"available_tools"`
	}
	endpoint := fmt.Sprintf("/api/agents/%s/%s/tools/count", url.PathEscape(agentFilename), url.PathEscape(agentName))
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp)
	if err != nil {
		return 0, err
	}

	return resp.AvailableTools, nil
}

// SessionStreamFrame is one frame of a session's event flow, as delivered by
// both streams the server exposes: a turn's own response
// ([Client.RunAgent]) and the session-scoped stream every attached client
// tails ([Client.StreamSessionEventsFrom]). Seq is the event's position in
// that session-scoped stream, identical on both, which is what lets a client
// recognise its own turn's events when it watches both.
//
// Exactly one of Event and Control is set. Control frames are stream
// bookkeeping rather than session events, and only ever appear on the
// session-scoped stream:
//
//   - [SessionStreamGap]: the requested resume point had already been evicted
//     from the server's buffer, so events were lost; re-read the snapshot.
//   - [SessionStreamExited]: the session ended server-side; stop tailing.
//
// A client can reconnect with the last Seq it saw to resume exactly where it
// left off. Seq is 0 for a control frame, and for a turn on a session that
// has no event log (nobody is watching it).
type SessionStreamFrame struct {
	Seq     uint64
	Event   Event
	Control string
}

const (
	SessionStreamGap    = "gap"
	SessionStreamExited = "session_exited"
)

// StreamSessionEvents streams events for a session as they occur via Server-Sent Events.
// The returned channel is closed when ctx is cancelled, the stream's max
// duration is reached, or the server closes the connection.
func (c *Client) StreamSessionEvents(ctx context.Context, sessionID string) (<-chan Event, error) {
	frames, err := c.StreamSessionEventsFrom(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}

	events := make(chan Event, defaultEventChannelCapacity)
	go func() {
		defer close(events)
		for frame := range frames {
			if frame.Event != nil {
				events <- frame.Event
			}
		}
	}()
	return events, nil
}

// StreamSessionEventsFrom is [Client.StreamSessionEvents] with sequence
// numbers and replay: events newer than since are replayed before live
// tailing resumes, so a client that reconnects with the last sequence number
// it saw misses nothing. Pass since=0 to start from whatever the server still
// has buffered.
//
// A single connection is bounded by the streaming timeout; the channel
// closing without a [SessionStreamExited] frame means the connection dropped,
// and the caller should reconnect from the last sequence number it saw.
func (c *Client) StreamSessionEventsFrom(ctx context.Context, sessionID string, since uint64) (<-chan SessionStreamFrame, error) {
	endpoint := fmt.Sprintf("/api/sessions/%s/events", sessionID)

	u := *c.baseURL
	u.Path = path.Join(u.Path, endpoint)
	if since > 0 {
		u.RawQuery = url.Values{"since": {strconv.FormatUint(since, 10)}}.Encode()
	}

	// Bound the maximum lifetime of a single SSE connection. The cancel
	// must be tied to the goroutine consuming the stream, not to this
	// function's return: cancelling streamCtx kills the in-flight HTTP
	// request, which would turn the stream into a one-shot read.
	timeout := c.timeoutFor("streaming")
	streamCtx, cancel := context.WithTimeout(ctx, timeout)

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // body is closed in the goroutine below
	if err != nil {
		cancel()
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer cancel()
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading error response body: %w", err)
		}

		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	frames := make(chan SessionStreamFrame, defaultEventChannelCapacity)
	go func() {
		defer cancel()
		c.readSSEFrames(ctx, resp.Body, frames, func() bool { return streamCtx.Err() != nil })
	}()
	return frames, nil
}

// GetSessionTools retrieves tools available in a session.
func (c *Client) GetSessionTools(ctx context.Context, sessionID string) ([]tools.Tool, error) {
	var toolList []tools.Tool
	endpoint := fmt.Sprintf("/api/sessions/%s/tools", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &toolList)
	return toolList, err
}

// GetAvailableModels returns available models for the agent.
func (c *Client) GetAvailableModels(ctx context.Context) ([]string, error) {
	var models []string
	err := c.doRequest(ctx, http.MethodGet, "/api/models", nil, &models)
	return models, err
}

// GetSessionMCPPrompts returns available MCP prompts for a session.
func (c *Client) GetSessionMCPPrompts(ctx context.Context, sessionID string) (map[string]any, error) {
	var prompts map[string]any
	endpoint := fmt.Sprintf("/api/sessions/%s/mcp/prompts", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &prompts)
	return prompts, err
}

// ExecuteSessionMCPPrompt executes an MCP prompt in a session.
func (c *Client) ExecuteSessionMCPPrompt(ctx context.Context, sessionID, promptName string, args map[string]string) (string, error) {
	endpoint := fmt.Sprintf("/api/sessions/%s/mcp/prompts/%s/execute", sessionID, promptName)
	var result struct {
		Result string `json:"result"`
	}
	err := c.doRequest(ctx, http.MethodPost, endpoint, args, &result)
	return result.Result, err
}

// GetSessionSkills returns available skills for a session.
func (c *Client) GetSessionSkills(ctx context.Context, sessionID string) (map[string]any, error) {
	var skills map[string]any
	endpoint := fmt.Sprintf("/api/sessions/%s/skills", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &skills)
	return skills, err
}

// CompactSession triggers session compaction on the server.
func (c *Client) CompactSession(ctx context.Context, sessionID string) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/compact", sessionID)
	return c.doRequest(ctx, http.MethodPost, endpoint, nil, nil)
}

// GetSessionToolsets returns toolset statuses for a session.
func (c *Client) GetSessionToolsets(ctx context.Context, sessionID string) ([]map[string]any, error) {
	var toolsets []map[string]any
	endpoint := fmt.Sprintf("/api/sessions/%s/toolsets", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &toolsets)
	return toolsets, err
}

// RestartSessionToolset restarts a toolset in a session.
func (c *Client) RestartSessionToolset(ctx context.Context, sessionID, toolsetName string) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/toolsets/%s/restart", sessionID, toolsetName)
	return c.doRequest(ctx, http.MethodPost, endpoint, nil, nil)
}

// PauseSession pauses a session.
func (c *Client) PauseSession(ctx context.Context, sessionID string) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/pause", sessionID)
	return c.doRequest(ctx, http.MethodPost, endpoint, nil, nil)
}

// GetSessionSnapshots retrieves snapshots for a session.
func (c *Client) GetSessionSnapshots(ctx context.Context, sessionID string) ([]map[string]any, error) {
	var snapshots []map[string]any
	endpoint := fmt.Sprintf("/api/sessions/%s/snapshots", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &snapshots)
	return snapshots, err
}

// UndoSession reverts a session to the previous snapshot.
func (c *Client) UndoSession(ctx context.Context, sessionID string) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/undo", sessionID)
	return c.doRequest(ctx, http.MethodPost, endpoint, nil, nil)
}

// ResetSession resets a session to initial state.
func (c *Client) ResetSession(ctx context.Context, sessionID string) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/reset", sessionID)
	return c.doRequest(ctx, http.MethodPost, endpoint, nil, nil)
}

// AddMessage adds a message to a session.
func (c *Client) AddMessage(ctx context.Context, sessionID string, msg *session.Message) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/messages", sessionID)
	req := api.AddMessageRequest{Message: msg}
	return c.doRequest(ctx, http.MethodPost, endpoint, req, nil)
}

// UpdateMessage updates a message in a session.
func (c *Client) UpdateMessage(ctx context.Context, sessionID, msgID string, msg *session.Message) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/messages/%s", sessionID, msgID)
	req := api.UpdateMessageRequest{Message: msg}
	return c.doRequest(ctx, http.MethodPatch, endpoint, req, nil)
}

// AddSummary adds a summary item to a session.
func (c *Client) AddSummary(ctx context.Context, sessionID string, item session.Item) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/summaries", sessionID)
	// Tokens mirrors FirstKeptEntry under its legacy wire name so older
	// servers keep understanding the request.
	req := api.AddSummaryRequest{
		Summary:        item.Summary,
		Tokens:         item.FirstKeptEntry,
		FirstKeptEntry: item.FirstKeptEntry,
		Cost:           item.Cost,
		Model:          item.Model,
		Usage:          item.Usage,
	}
	return c.doRequest(ctx, http.MethodPost, endpoint, req, nil)
}

// UpdateSessionTokens updates token counts for a session.
func (c *Client) UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, cost float64) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/tokens", sessionID)
	req := api.UpdateSessionTokensRequest{InputTokens: inputTokens, OutputTokens: outputTokens, Cost: cost}
	return c.doRequest(ctx, http.MethodPatch, endpoint, req, nil)
}

// SetSessionStarred sets the starred status for a session.
func (c *Client) SetSessionStarred(ctx context.Context, sessionID string, starred bool) error {
	endpoint := fmt.Sprintf("/api/sessions/%s/starred", sessionID)
	req := api.SetSessionStarredRequest{Starred: starred}
	return c.doRequest(ctx, http.MethodPatch, endpoint, req, nil)
}

// Health checks the health of the remote server.
func (c *Client) Health(ctx context.Context) error {
	var resp api.HealthResponse
	return c.doRequest(ctx, http.MethodGet, "/health", nil, &resp)
}

// Ready checks if the remote server is ready to handle requests.
func (c *Client) Ready(ctx context.Context) (*api.ReadyResponse, error) {
	var resp api.ReadyResponse
	if err := c.doRequest(ctx, http.MethodGet, "/ready", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSessionRecoveryData retrieves recovery data for a session in case of store failure
func (c *Client) GetSessionRecoveryData(ctx context.Context, sessionID string) (map[string]any, error) {
	var data map[string]any
	endpoint := fmt.Sprintf("/api/sessions/%s/recovery", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &data)
	return data, err
}

// BatchDeleteSessions deletes multiple sessions in a single operation
func (c *Client) BatchDeleteSessions(ctx context.Context, sessionIDs []string) (map[string]any, error) {
	var resp map[string]any
	req := api.BatchDeleteSessionsRequest{SessionIDs: sessionIDs}
	err := c.doRequest(ctx, http.MethodPost, "/api/sessions/batch/delete", req, &resp)
	return resp, err
}

// BatchExportSessions exports multiple sessions
func (c *Client) BatchExportSessions(ctx context.Context, sessionIDs []string, format string) (map[string]any, error) {
	var resp map[string]any
	req := api.BatchExportSessionsRequest{SessionIDs: sessionIDs, Format: format}
	err := c.doRequest(ctx, http.MethodPost, "/api/sessions/batch/export", req, &resp)
	return resp, err
}

// GetSessionQueueStatus retrieves the queue depth and capacity for a session
func (c *Client) GetSessionQueueStatus(ctx context.Context, sessionID string) (*api.QueueDepthResponse, error) {
	var resp api.QueueDepthResponse
	endpoint := fmt.Sprintf("/api/sessions/%s/queue", sessionID)
	err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp)
	return &resp, err
}

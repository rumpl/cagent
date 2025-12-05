package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

type mockToolEventPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockToolEventPublisher) Publish(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *mockToolEventPublisher) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event{}, m.events...)
}

func TestToolExecutor_RegisterHandler(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool"}
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		return &tools.ToolCallResult{Output: "test output"}, nil
	}

	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	_, exists := executor.toolMap["test_tool"]
	require.True(t, exists, "handler should be registered")
}

func TestToolExecutor_Resume(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	var received ResumeType
	done := make(chan struct{})

	go func() {
		received = <-executor.resumeChan
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	executor.Resume(t.Context(), ResumeTypeApprove)

	select {
	case <-done:
		assert.Equal(t, ResumeTypeApprove, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for resume signal")
	}
}

func TestToolExecutor_ResumeApproveSession(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	var received ResumeType
	done := make(chan struct{})

	go func() {
		received = <-executor.resumeChan
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	executor.Resume(t.Context(), ResumeTypeApproveSession)

	select {
	case <-done:
		assert.Equal(t, ResumeTypeApproveSession, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for resume signal")
	}
}

func TestToolExecutor_ResumeReject(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	var received ResumeType
	done := make(chan struct{})

	go func() {
		received = <-executor.resumeChan
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)

	executor.Resume(t.Context(), ResumeTypeReject)

	select {
	case <-done:
		assert.Equal(t, ResumeTypeReject, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for resume signal")
	}
}

func TestToolExecutor_ResumeNoReceiver(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	executor.Resume(t.Context(), ResumeTypeApprove)
}

func TestToolExecutor_ProcessToolCalls_RegisteredTool_Approved(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool", Annotations: tools.ToolAnnotations{ReadOnlyHint: true}}
	handlerCalled := false
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		handlerCalled = true
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	require.True(t, handlerCalled, "handler should have been called")

	events := publisher.Events()
	require.GreaterOrEqual(t, len(events), 2)

	hasToolCall := false
	hasToolCallResponse := false
	for _, ev := range events {
		if _, ok := ev.(*ToolCallEvent); ok {
			hasToolCall = true
		}
		if _, ok := ev.(*ToolCallResponseEvent); ok {
			hasToolCallResponse = true
		}
	}
	assert.True(t, hasToolCall, "should have ToolCallEvent")
	assert.True(t, hasToolCallResponse, "should have ToolCallResponseEvent")
}

func TestToolExecutor_ProcessToolCalls_AgentTool_Approved(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	handlerCalled := false
	agentTool := tools.Tool{
		Name:        "agent_tool",
		Annotations: tools.ToolAnnotations{ReadOnlyHint: true},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			handlerCalled = true
			return &tools.ToolCallResult{Output: "agent output"}, nil
		},
	}

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "agent_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, []tools.Tool{agentTool}, testAgent, eventsChan)

	require.True(t, handlerCalled, "agent tool handler should have been called")
}

func TestToolExecutor_ProcessToolCalls_ToolsApproved(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool"}
	handlerCalled := false
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		handlerCalled = true
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	sess.ToolsApproved = true
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	require.True(t, handlerCalled, "handler should have been called")
}

func TestToolExecutor_ProcessToolCalls_WaitForApproval(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool"}
	handlerCalled := false
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		handlerCalled = true
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	done := make(chan struct{})
	go func() {
		executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	events := publisher.Events()
	hasConfirmation := false
	for _, ev := range events {
		if _, ok := ev.(*ToolCallConfirmationEvent); ok {
			hasConfirmation = true
			break
		}
	}
	require.True(t, hasConfirmation, "should have emitted confirmation event")

	executor.Resume(t.Context(), ResumeTypeApprove)

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for process to complete")
	}

	require.True(t, handlerCalled, "handler should have been called after approval")
}

func TestToolExecutor_ProcessToolCalls_Rejection(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool"}
	handlerCalled := false
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		handlerCalled = true
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	done := make(chan struct{})
	go func() {
		executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	executor.Resume(t.Context(), ResumeTypeReject)

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for process to complete")
	}

	require.False(t, handlerCalled, "handler should not have been called after rejection")

	events := publisher.Events()
	hasRejectionResponse := false
	for _, ev := range events {
		if resp, ok := ev.(*ToolCallResponseEvent); ok {
			if resp.Response == "The user rejected the tool call." {
				hasRejectionResponse = true
			}
		}
	}
	assert.True(t, hasRejectionResponse, "should have emitted rejection response")
}

func TestToolExecutor_ProcessToolCalls_ContextCancellation(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool"}
	handlerCalled := false
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		handlerCalled = true
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
		{ID: "call_2", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		executor.ProcessToolCalls(ctx, sess, calls, nil, testAgent, eventsChan)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for process to complete")
	}

	require.False(t, handlerCalled, "handler should not have been called after cancellation")

	events := publisher.Events()
	cancellationCount := 0
	for _, ev := range events {
		if resp, ok := ev.(*ToolCallResponseEvent); ok {
			if resp.Response == "The tool call was canceled by the user." {
				cancellationCount++
			}
		}
	}
	assert.GreaterOrEqual(t, cancellationCount, 1, "should have emitted cancellation response(s)")
}

func TestToolExecutor_ProcessToolCalls_HandlerError(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool", Annotations: tools.ToolAnnotations{ReadOnlyHint: true}}
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		return nil, errors.New("handler error")
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	events := publisher.Events()
	hasErrorResponse := false
	for _, ev := range events {
		if resp, ok := ev.(*ToolCallResponseEvent); ok {
			if resp.Response == "error calling tool: handler error" {
				hasErrorResponse = true
			}
		}
	}
	assert.True(t, hasErrorResponse, "should have emitted error response")
}

func TestToolExecutor_ProcessToolCalls_EmptyOutput(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	tool := tools.Tool{Name: "test_tool", Annotations: tools.ToolAnnotations{ReadOnlyHint: true}}
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		return &tools.ToolCallResult{Output: ""}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	messages := sess.GetAllMessages()
	require.Len(t, messages, 1)
	assert.Equal(t, "(no output)", messages[0].Message.Content)
}

func TestToolExecutor_ProcessToolCalls_MultipleCalls(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	callCount := 0
	tool := tools.Tool{Name: "test_tool", Annotations: tools.ToolAnnotations{ReadOnlyHint: true}}
	handler := func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error) {
		callCount++
		return &tools.ToolCallResult{Output: "success"}, nil
	}
	executor.RegisterHandler("test_tool", ToolHandler{Handler: handler, Tool: tool})

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
		{ID: "call_2", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
		{ID: "call_3", Type: "function", Function: tools.FunctionCall{Name: "test_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	assert.Equal(t, 3, callCount, "handler should have been called 3 times")
}

func TestToolExecutor_ProcessToolCalls_UnknownTool(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "unknown_tool", Arguments: "{}"}},
	}

	executor.ProcessToolCalls(t.Context(), sess, calls, nil, testAgent, eventsChan)

	messages := sess.GetAllMessages()
	assert.Empty(t, messages, "no messages should be added for unknown tool")
}

func TestToolExecutor_ProcessToolCalls_AgentTool_WaitForApproval(t *testing.T) {
	publisher := &mockToolEventPublisher{}
	tracing := newTracingProvider(nil)
	executor := newToolExecutor(publisher, tracing)

	handlerCalled := false
	agentTool := tools.Tool{
		Name:        "agent_tool",
		Annotations: tools.ToolAnnotations{ReadOnlyHint: false},
		Handler: func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
			handlerCalled = true
			return &tools.ToolCallResult{Output: "agent output"}, nil
		},
	}

	sess := session.New()
	testAgent := agent.New("test-agent", "test system prompt")
	eventsChan := make(chan Event, 10)

	calls := []tools.ToolCall{
		{ID: "call_1", Type: "function", Function: tools.FunctionCall{Name: "agent_tool", Arguments: "{}"}},
	}

	done := make(chan struct{})
	go func() {
		executor.ProcessToolCalls(t.Context(), sess, calls, []tools.Tool{agentTool}, testAgent, eventsChan)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	executor.Resume(t.Context(), ResumeTypeApproveSession)

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for process to complete")
	}

	require.True(t, handlerCalled, "agent tool handler should have been called after approval")
	assert.True(t, sess.ToolsApproved, "session should have tools approved")
}

package runtime

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

type testMessageStream struct {
	responses []chat.MessageStreamResponse
	index     int
	closed    bool
}

func (m *testMessageStream) Recv() (chat.MessageStreamResponse, error) {
	if m.index >= len(m.responses) {
		return chat.MessageStreamResponse{}, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *testMessageStream) Close() {
	m.closed = true
}

func TestStreamProcessor_ProcessStream_SimpleContent(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Hello "}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "World"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "Hello World", result.Content)
	assert.True(t, result.Stopped)

	events := publisher.Events()
	assert.Len(t, events, 2)

	assert.True(t, stream.closed)
}

func TestStreamProcessor_ProcessStream_ToolCalls(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{
						Delta: chat.MessageDelta{
							ToolCalls: []tools.ToolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: tools.FunctionCall{
										Name:      "test_tool",
										Arguments: `{"arg": "value"}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	agentTools := []tools.Tool{
		{Name: "test_tool"},
	}

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		agentTools,
		sess,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, result.Calls, 1)
	assert.Equal(t, "test_tool", result.Calls[0].Function.Name)

	events := publisher.Events()
	hasPartialEvent := false
	for _, e := range events {
		if _, ok := e.(*PartialToolCallEvent); ok {
			hasPartialEvent = true
			break
		}
	}
	assert.True(t, hasPartialEvent, "expected PartialToolCallEvent to be published")
}

func TestStreamProcessor_ProcessStream_EmptyResponse(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.True(t, result.Stopped, "expected Stopped to be true due to no output")
}

func TestStreamProcessor_ProcessStream_ReasoningContent(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{ReasoningContent: "Let me think..."}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "The answer is 42."}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "The answer is 42.", result.Content)
	assert.Equal(t, "Let me think...", result.ReasoningContent)

	events := publisher.Events()
	assert.Len(t, events, 2)
	_, isReasoningEvent := events[0].(*AgentChoiceReasoningEvent)
	assert.True(t, isReasoningEvent)
}

func TestStreamProcessor_ProcessStream_UsageTracking(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Hello"}},
				},
				Usage: &chat.Usage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	model := &modelsdev.Model{
		Name: "test-model",
		Cost: &modelsdev.Cost{
			Input:  0.001,
			Output: 0.002,
		},
	}

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		model,
	)

	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Content)
	assert.Equal(t, int64(100), sess.InputTokens)
	assert.Equal(t, int64(50), sess.OutputTokens)
}

func TestStreamProcessor_ProcessStream_ThinkingSignature(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{ThinkingSignature: "sig123"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Answer"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "sig123", result.ThinkingSignature)
}

func TestStreamProcessor_ProcessStream_ThoughtSignature(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{ThoughtSignature: []byte("thought-sig")}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Answer"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, []byte("thought-sig"), result.ThoughtSignature)
}

func TestStreamProcessor_ProcessStream_FragmentedToolArgs(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{
						Delta: chat.MessageDelta{
							ToolCalls: []tools.ToolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: tools.FunctionCall{
										Name:      "my_tool",
										Arguments: `{"key":`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{
						Delta: chat.MessageDelta{
							ToolCalls: []tools.ToolCall{
								{
									ID: "call_1",
									Function: tools.FunctionCall{
										Arguments: `"value"}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	agentTools := []tools.Tool{
		{Name: "my_tool"},
	}

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		agentTools,
		sess,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, result.Calls, 1)
	assert.Equal(t, "my_tool", result.Calls[0].Function.Name)
	assert.JSONEq(t, `{"key":"value"}`, result.Calls[0].Function.Arguments)
}

func TestStreamProcessor_ProcessStream_MultipleToolCalls(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{
						Delta: chat.MessageDelta{
							ToolCalls: []tools.ToolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: tools.FunctionCall{
										Name:      "tool_one",
										Arguments: `{"a": 1}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{
						Delta: chat.MessageDelta{
							ToolCalls: []tools.ToolCall{
								{
									ID:   "call_2",
									Type: "function",
									Function: tools.FunctionCall{
										Name:      "tool_two",
										Arguments: `{"b": 2}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	agentTools := []tools.Tool{
		{Name: "tool_one"},
		{Name: "tool_two"},
	}

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		agentTools,
		sess,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, result.Calls, 2)
	assert.Equal(t, "tool_one", result.Calls[0].Function.Name)
	assert.Equal(t, "tool_two", result.Calls[1].Function.Name)

	events := publisher.Events()
	partialCount := 0
	for _, e := range events {
		if _, ok := e.(*PartialToolCallEvent); ok {
			partialCount++
		}
	}
	assert.Equal(t, 2, partialCount)
}

func TestStreamProcessor_ProcessStream_FinishReasonLength(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Truncated content"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonLength},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "Truncated content", result.Content)
	assert.True(t, result.Stopped)
}

func TestStreamProcessor_ProcessStream_EmptyChoices(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &testMessageStream{
		responses: []chat.MessageStreamResponse{
			{
				Choices: []chat.MessageStreamChoice{},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "Content"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
			},
		},
	}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "Content", result.Content)
}

func TestStreamProcessor_ProcessStream_StreamError(t *testing.T) {
	publisher := &mockEventPublisher{}
	processor := newStreamProcessor(publisher)

	stream := &errorStream{err: assert.AnError}

	testAgent := agent.New("test", "test system prompt")
	sess := session.New()

	result, err := processor.ProcessStream(
		t.Context(),
		stream,
		testAgent,
		nil,
		sess,
		nil,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error receiving from stream")
	assert.True(t, result.Stopped)
}

type errorStream struct {
	err    error
	closed bool
}

func (e *errorStream) Recv() (chat.MessageStreamResponse, error) {
	return chat.MessageStreamResponse{}, e.err
}

func (e *errorStream) Close() {
	e.closed = true
}

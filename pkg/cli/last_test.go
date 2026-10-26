package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/structuredoutput"
)

func TestRunLast(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		events []contract.Event
		json   bool
		want   string
	}{
		{
			name: "streamed final message",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking", "first"),
				contract.AgentChoice("root", "sess", "intermediate", "first"),
				contract.ToolCall(tools.ToolCall{}, tools.Tool{}, "root"),
				contract.ToolCallResponse("tool", tools.Tool{}, tools.ResultSuccess("tool output"), "tool output", "root"),
				contract.AgentChoice("root", "sess", "final ", "final"),
				contract.AgentChoice("root", "sess", "answer", "final"),
				contract.Warning("cache miss", "root"),
			},
			want: "final answer\n",
		},
		{
			name: "agent handoff and sub-session filtering",
			events: []contract.Event{
				contract.AgentChoice("root", "sess", "draft", "first"),
				contract.AgentChoice("reviewer", "sess", "reviewed", "final"),
				contract.AgentChoice("child", "child-session", "child answer", "child"),
				contract.StreamStopped("child-session", "child", "error"),
				contract.StreamStopped("sess", "reviewer", runtime.TurnEndReasonNormal),
			},
			want: "reviewed\n",
		},
		{
			name: "legacy events without message IDs",
			events: []contract.Event{
				contract.AgentChoice("root", "sess", "draft"),
				contract.AgentChoice("reviewer", "sess", "reviewed "),
				contract.AgentChoice("reviewer", "sess", "answer"),
			},
			want: "reviewed answer\n",
		},
		{
			name:   "JSON text is encoded as a string",
			json:   true,
			events: []contract.Event{contract.AgentChoice("root", "sess", "final\n\"answer\"", "final")},
			want:   "\"final\\n\\\"answer\\\"\"\n",
		},
		{
			name:   "structured JSON is not double encoded",
			json:   true,
			events: []contract.Event{contract.AgentChoice("root", "sess", "{\n  \"answer\": 9007199254740993\n}", "final")},
			want:   "{\"answer\":9007199254740993}\n",
		},
		{
			name:   "structured output in text mode stays unchanged",
			events: []contract.Event{contract.AgentChoice("root", "sess", "{\n  \"answer\": 4\n}", "final")},
			want:   "{\n  \"answer\": 4\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := Run(t.Context(), NewPrinter(&out), Config{Last: true, OutputJSON: tc.json},
				&mockRuntime{events: tc.events}, session.New(session.WithID("sess")), []string{"hello"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out.String())
		})
	}
}

func TestRunLastOnlyPrintsFinalTurn(t *testing.T) {
	t.Parallel()
	for _, outputJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[outputJSON], func(t *testing.T) {
			t.Parallel()
			var turn int
			rt := &mockRuntime{runStreamFn: func(_ context.Context, sess *session.Session) <-chan contract.Event {
				turn++
				ch := make(chan contract.Event, 1)
				ch <- contract.AgentChoice("root", sess.ID, []string{"first answer", "last answer"}[turn-1], "message")
				close(ch)
				return ch
			}}
			var out bytes.Buffer
			err := Run(t.Context(), NewPrinter(&out), Config{Last: true, OutputJSON: outputJSON}, rt, session.New(), []string{"one", "two"})
			require.NoError(t, err)
			want := "last answer\n"
			if outputJSON {
				want = "\"last answer\"\n"
			}
			assert.Equal(t, want, out.String())
		})
	}
}

func TestRunLastDoesNotPrintUnfinishedAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		events []contract.Event
		want   string
	}{
		{"empty", nil, "no final answer"},
		{"reasoning only", []contract.Event{contract.AgentChoiceReasoning("root", "sess", "thinking", "id")}, "no final answer"},
		{"runtime error", []contract.Event{contract.AgentChoice("root", "sess", "partial", "id"), contract.Error("model failed")}, "model failed"},
		{"iteration limit", []contract.Event{contract.AgentChoice("root", "sess", "partial", "id"), maxIterEvent(10)}, "no final answer"},
		{"canceled", []contract.Event{contract.AgentChoice("root", "sess", "partial", "id"), contract.StreamStopped("sess", "root", runtime.TurnEndReasonCanceled)}, "canceled"},
		{"empty final response", []contract.Event{contract.AgentChoice("root", "sess", "draft", "draft"), contract.AgentChoice("root", "sess", "", "final")}, "no final answer"},
		{"reasoning only final response", []contract.Event{contract.AgentChoice("root", "sess", "draft", "draft"), contract.AgentChoiceReasoning("reviewer", "sess", "thinking", "final")}, "no final answer"},
		{"remote tool preamble without usage", []contract.Event{contract.AgentChoice("root", "sess", "calling tool", "id"), &contract.MessageAddedEvent{SessionID: "sess", HasToolCalls: true}}, "no final answer"},
		{"empty completion without usage", []contract.Event{contract.AgentChoice("root", "sess", "draft", "id"), &contract.TokenUsageEvent{SessionID: "sess", AssistantMessageEmpty: true}}, "no final answer"},
		{"remote empty completion", []contract.Event{contract.AgentChoice("root", "sess", "draft", "id"), &contract.MessageAddedEvent{SessionID: "sess", AssistantMessageEmpty: true}}, "no final answer"},
		{"truncated", []contract.Event{contract.AgentChoice("root", "sess", "partial", "id"), &contract.StreamStoppedEvent{SessionID: "sess", Reason: runtime.TurnEndReasonNormal, FinishReason: chat.FinishReasonLength}}, "truncated"},
		{"tool preamble", []contract.Event{contract.AgentChoice("root", "sess", "calling tool", "id"), contract.NewTokenUsageEvent("sess", "root", &contract.Usage{LastMessage: &contract.MessageUsage{FinishReason: chat.FinishReasonToolCalls}})}, "no final answer"},
	} {
		for _, outputJSON := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/text", true: "/json"}[outputJSON], func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				err := Run(t.Context(), NewPrinter(&out), Config{Last: true, OutputJSON: outputJSON},
					&mockRuntime{events: tc.events}, session.New(session.WithID("sess")), []string{"hello"})
				require.ErrorContains(t, err, tc.want)
				assert.Empty(t, out.String())
			})
		}
	}
}

func TestRunLastDeclinesInteractions(t *testing.T) {
	t.Parallel()
	rt := &mockRuntime{events: []contract.Event{
		&contract.ToolCallConfirmationEvent{},
		&contract.ElicitationRequestEvent{ElicitationID: "request"},
		contract.AgentChoice("root", "sess", "answer", "id"),
	}}
	var out bytes.Buffer
	require.NoError(t, Run(t.Context(), NewPrinter(&out), Config{Last: true, AutoApprove: true}, rt, session.New(session.WithID("sess")), []string{"hello"}))
	assert.Equal(t, "answer\n", out.String())
	require.Len(t, rt.getResumes(), 1)
	assert.Equal(t, runtime.ResumeTypeReject, rt.getResumes()[0].Type)
	assert.Equal(t, 1, rt.elicitationDeclines)
}

func TestRunLastReadsStdin(t *testing.T) {
	for _, messages := range [][]string{nil, {"-"}} {
		t.Run(strings.Join(messages, ""), func(t *testing.T) {
			swapStdin(t, "hello\n")
			rt := &mockRuntime{events: []contract.Event{contract.AgentChoice("root", "sess", "answer", "id")}}
			var out bytes.Buffer
			require.NoError(t, Run(t.Context(), NewPrinter(&out), Config{Last: true}, rt, session.New(session.WithID("sess")), messages))
			assert.Equal(t, "answer\n", out.String())
		})
	}
}

func TestLastResponseWriteFailure(t *testing.T) {
	t.Parallel()
	for _, outputJSON := range []bool{false, true} {
		r := lastResponse{}
		r.content.WriteString("answer")
		require.ErrorIs(t, r.print(failingWriter{}, outputJSON), io.ErrClosedPipe)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRunLastToolStructuredOutput(t *testing.T) {
	t.Parallel()
	for _, outputJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[outputJSON], func(t *testing.T) {
			t.Parallel()
			model := &outputToolModel{}
			a := agent.New("root", "test", agent.WithModel(model), agent.WithStructuredOutput(&latest.StructuredOutput{
				Name: "answer", Mode: latest.StructuredOutputModeTool,
				Schema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "integer"}}, "required": []string{"answer"}},
			}))
			rt, err := runtime.NewLocalRuntime(t.Context(), team.New(team.WithAgents(a)), runtime.WithSessionCompaction(false), runtime.WithModelStore(noPricing{}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = rt.Close() })
			var out bytes.Buffer
			err = Run(t.Context(), NewPrinter(&out), Config{Last: true, OutputJSON: outputJSON}, rt, session.New(session.WithNonInteractive(true)), []string{"hello"})
			require.NoError(t, err)
			var answer map[string]int
			require.NoError(t, json.Unmarshal(out.Bytes(), &answer))
			assert.Equal(t, map[string]int{"answer": 4}, answer)
		})
	}
}

type outputToolModel struct{ fixedModel }

func (*outputToolModel) CreateChatCompletionStream(context.Context, []chat.Message, []tools.Tool) (chat.MessageStream, error) {
	return &outputToolStream{}, nil
}

type outputToolStream struct{ step int }

func (s *outputToolStream) Recv() (chat.MessageStreamResponse, error) {
	s.step++
	switch s.step {
	case 1:
		return chat.MessageStreamResponse{Choices: []chat.MessageStreamChoice{{Delta: chat.MessageDelta{
			Content: "Here is the report", ReasoningContent: "thinking",
			ToolCalls: []tools.ToolCall{{ID: "output", Type: "function", Function: tools.FunctionCall{Name: structuredoutput.ToolName, Arguments: `{"answer":4}`}}},
		}}}}, nil
	case 2:
		return chat.MessageStreamResponse{Choices: []chat.MessageStreamChoice{{FinishReason: chat.FinishReasonToolCalls}}, Usage: &chat.Usage{InputTokens: 1, OutputTokens: 1}}, nil
	default:
		return chat.MessageStreamResponse{}, io.EOF
	}
}

func (*outputToolStream) Close() {}

func TestRunLastFailsBeforeStartingFollowUpTurn(t *testing.T) {
	t.Parallel()
	for _, trigger := range []contract.Event{maxIterEvent(10), contract.StreamStopped("sess", "root", runtime.TurnEndReasonCanceled)} {
		var turns int
		rt := &mockRuntime{runStreamFn: func(context.Context, *session.Session) <-chan contract.Event {
			turns++
			ch := make(chan contract.Event, 2)
			ch <- contract.AgentChoice("root", "sess", "partial", "id")
			ch <- trigger
			close(ch)
			return ch
		}}
		var out bytes.Buffer
		err := Run(t.Context(), NewPrinter(&out), Config{Last: true}, rt, session.New(session.WithID("sess")), []string{"one", "two"})
		require.Error(t, err)
		assert.Equal(t, 1, turns)
		assert.Empty(t, out.String())
	}
}

func TestRunLastForceHandoff(t *testing.T) {
	t.Parallel()
	for _, reply := range []string{"reviewed answer", ""} {
		t.Run(reply, func(t *testing.T) {
			t.Parallel()
			reviewer := agent.New("reviewer", "test", agent.WithModel(&fixedModel{reply: reply}))
			root := agent.New("root", "test", agent.WithModel(&fixedModel{reply: "draft"}), agent.WithForceHandoff(reviewer))
			rt, err := runtime.NewLocalRuntime(t.Context(), team.New(team.WithAgents(root, reviewer)), runtime.WithSessionCompaction(false), runtime.WithModelStore(noPricing{}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = rt.Close() })
			var out bytes.Buffer
			err = Run(t.Context(), NewPrinter(&out), Config{Last: true}, rt, session.New(session.WithNonInteractive(true)), []string{"hello"})
			if reply == "" {
				require.ErrorContains(t, err, "no final answer")
				assert.Empty(t, out.String())
			} else {
				require.NoError(t, err)
				assert.Equal(t, reply+"\n", out.String())
			}
		})
	}
}

package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestRunSeparatesReasoningFromResponse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		events        []contract.Event
		hideToolCalls bool
		tty           bool
		want          string
	}{
		{
			name: "streamed reasoning and response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "Let's keep it "),
				contract.AgentChoiceReasoning("root", "sess", "simple and respectful!"),
				contract.AgentChoice("root", "sess", "Could you please "),
				contract.AgentChoice("root", "sess", "make this clearer?"),
			},
			want: "Let's keep it simple and respectful!\n\nCould you please make this clearer?",
		},
		{
			name: "response without reasoning",
			events: []contract.Event{
				contract.AgentChoice("root", "sess", "Could you please "),
				contract.AgentChoice("root", "sess", "make this clearer?"),
			},
			want: "Could you please make this clearer?",
		},
		{
			name: "empty reasoning",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", ""),
				contract.AgentChoice("root", "sess", "answer"),
				contract.AgentChoiceReasoning("root", "sess", ""),
				contract.AgentChoice("root", "sess", " continues"),
			},
			want: "answer continues",
		},
		{
			name: "empty response chunks",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("root", "sess", ""),
				contract.AgentChoiceReasoning("root", "sess", " continues"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			want: "thinking continues\n\nanswer",
		},
		{
			name: "reasoning only",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("root", "sess", ""),
			},
			want: "thinking",
		},
		{
			name: "non-printing event between reasoning and response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.NewTokenUsageEvent("sess", "root", &contract.Usage{}),
				contract.AgentChoice("root", "sess", "answer"),
			},
			want: "thinking\n\nanswer",
		},
		{
			name: "multiple reasoning blocks",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("root", "sess", "preamble"),
				contract.ToolCall(tools.ToolCall{ID: "call", Function: tools.FunctionCall{Name: "lookup"}}, tools.Tool{}, "root"),
				contract.AgentChoiceReasoning("root", "sess", "more thinking"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			want: "thinking\n\npreamble\nCalling lookup()\nmore thinking\n\nanswer",
		},
		{
			name: "visible tool output already separates response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.ToolCall(tools.ToolCall{ID: "call", Function: tools.FunctionCall{Name: "lookup"}}, tools.Tool{}, "root"),
				contract.ToolCallResponse("call", tools.Tool{Name: "lookup"}, tools.ResultSuccess("result"), "result", "root"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			want: "thinking\nCalling lookup()\n\nlookup response → \"result\"\nanswer",
		},
		{
			name: "hidden tools do not consume separator",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.ToolCall(tools.ToolCall{}, tools.Tool{}, "root"),
				contract.ToolCallResponse("call", tools.Tool{}, tools.ResultSuccess("result"), "result", "root"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			hideToolCalls: true,
			want:          "thinking\n\nanswer",
		},
		{
			name: "warning already separates response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.Warning("notice", "root"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			want: "thinking\n⚠️  notice\nanswer",
		},
		{
			name: "TTY thinking and response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("root", "sess", "answer"),
			},
			tty:  true,
			want: "\n--- Agent: " + bold("root") + " ---\nthinking\n\nanswer",
		},
		{
			name: "TTY agent switch already separates response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("reviewer", "sess", "answer"),
			},
			tty:  true,
			want: "\n--- Agent: " + bold("root") + " ---\nthinking\n\n--- Agent: " + bold("reviewer") + " ---\nanswer",
		},
		{
			name: "agent switch already separates response",
			events: []contract.Event{
				contract.AgentChoiceReasoning("root", "sess", "thinking"),
				contract.AgentChoice("reviewer", "sess", "answer"),
			},
			want: "thinking\nanswer",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			out := NewPrinter(&buf)
			out.isTTYOut = tc.tty
			err := Run(t.Context(), out, Config{HideToolCalls: tc.hideToolCalls},
				&mockRuntime{events: tc.events}, session.New(), []string{"Make this clearer"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

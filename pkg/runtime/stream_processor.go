package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tools"
)

type streamProcessor struct {
	events EventPublisher
}

func newStreamProcessor(events EventPublisher) *streamProcessor {
	return &streamProcessor{
		events: events,
	}
}

func (p *streamProcessor) ProcessStream(
	ctx context.Context,
	stream chat.MessageStream,
	a *agent.Agent,
	agentTools []tools.Tool,
	sess *session.Session,
	m *modelsdev.Model,
) (streamResult, error) {
	defer stream.Close()

	var fullContent strings.Builder
	var fullReasoningContent strings.Builder
	var thinkingSignature string
	var thoughtSignature []byte
	var toolCalls []tools.ToolCall
	emittedPartialEvents := make(map[string]bool)

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return streamResult{Stopped: true}, fmt.Errorf("error receiving from stream: %w", err)
		}

		p.processUsage(ctx, &response, sess, m)

		if len(response.Choices) == 0 {
			continue
		}
		choice := response.Choices[0]

		if len(choice.Delta.ThoughtSignature) > 0 {
			thoughtSignature = choice.Delta.ThoughtSignature
		}

		if choice.FinishReason == chat.FinishReasonStop || choice.FinishReason == chat.FinishReasonLength {
			return streamResult{
				Calls:             toolCalls,
				Content:           fullContent.String(),
				ReasoningContent:  fullReasoningContent.String(),
				ThinkingSignature: thinkingSignature,
				ThoughtSignature:  thoughtSignature,
				Stopped:           true,
			}, nil
		}

		if len(choice.Delta.ToolCalls) > 0 {
			p.processToolCallDeltas(choice.Delta.ToolCalls, &toolCalls, agentTools, a, emittedPartialEvents)
			continue
		}

		if choice.Delta.ReasoningContent != "" {
			p.events.Publish(AgentChoiceReasoning(a.Name(), choice.Delta.ReasoningContent))
			fullReasoningContent.WriteString(choice.Delta.ReasoningContent)
		}

		if choice.Delta.ThinkingSignature != "" {
			thinkingSignature = choice.Delta.ThinkingSignature
		}

		if choice.Delta.Content != "" {
			p.events.Publish(AgentChoice(a.Name(), choice.Delta.Content))
			fullContent.WriteString(choice.Delta.Content)
		}
	}

	stoppedDueToNoOutput := fullContent.Len() == 0 && len(toolCalls) == 0

	return streamResult{
		Calls:             toolCalls,
		Content:           fullContent.String(),
		ReasoningContent:  fullReasoningContent.String(),
		ThinkingSignature: thinkingSignature,
		ThoughtSignature:  thoughtSignature,
		Stopped:           stoppedDueToNoOutput,
	}, nil
}

func (p *streamProcessor) processUsage(ctx context.Context, response *chat.MessageStreamResponse, sess *session.Session, m *modelsdev.Model) {
	if response.Usage == nil {
		return
	}

	if m != nil {
		cost := float64(response.Usage.InputTokens)*m.Cost.Input +
			float64(response.Usage.OutputTokens)*m.Cost.Output +
			float64(response.Usage.CachedInputTokens)*m.Cost.CacheRead +
			float64(response.Usage.CacheWriteTokens)*m.Cost.CacheWrite
		sess.Cost += cost / 1e6
	}

	sess.InputTokens = response.Usage.InputTokens + response.Usage.CachedInputTokens + response.Usage.CacheWriteTokens
	sess.OutputTokens = response.Usage.OutputTokens

	modelName := "unknown"
	if m != nil {
		modelName = m.Name
	}
	telemetry.RecordTokenUsage(ctx, modelName, sess.InputTokens, sess.OutputTokens, sess.Cost)
}

func (p *streamProcessor) processToolCallDeltas(
	deltas []tools.ToolCall,
	toolCalls *[]tools.ToolCall,
	agentTools []tools.Tool,
	a *agent.Agent,
	emittedPartialEvents map[string]bool,
) {
	for _, deltaToolCall := range deltas {
		idx := -1
		for i, toolCall := range *toolCalls {
			if toolCall.ID == deltaToolCall.ID {
				idx = i
				break
			}
		}

		if idx == -1 {
			idx = len(*toolCalls)
			*toolCalls = append(*toolCalls, tools.ToolCall{
				ID:   deltaToolCall.ID,
				Type: deltaToolCall.Type,
			})
		}

		shouldEmitPartial := !emittedPartialEvents[deltaToolCall.ID] &&
			deltaToolCall.Function.Name != "" &&
			(*toolCalls)[idx].Function.Name == ""

		if deltaToolCall.ID != "" {
			(*toolCalls)[idx].ID = deltaToolCall.ID
		}
		if deltaToolCall.Type != "" {
			(*toolCalls)[idx].Type = deltaToolCall.Type
		}
		if deltaToolCall.Function.Name != "" {
			(*toolCalls)[idx].Function.Name = deltaToolCall.Function.Name
		}
		if deltaToolCall.Function.Arguments != "" {
			if (*toolCalls)[idx].Function.Arguments == "" {
				(*toolCalls)[idx].Function.Arguments = deltaToolCall.Function.Arguments
			} else {
				(*toolCalls)[idx].Function.Arguments += deltaToolCall.Function.Arguments
			}
			shouldEmitPartial = true
		}

		if shouldEmitPartial {
			tool := p.findTool((*toolCalls)[idx].Function.Name, agentTools)
			p.events.Publish(PartialToolCall((*toolCalls)[idx], tool, a.Name()))
			emittedPartialEvents[deltaToolCall.ID] = true
		}
	}
}

func (p *streamProcessor) findTool(name string, agentTools []tools.Tool) tools.Tool {
	for _, t := range agentTools {
		if t.Name == name {
			return t
		}
	}
	return tools.Tool{}
}

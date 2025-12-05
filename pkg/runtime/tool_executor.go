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
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tools"
)

type toolExecutor struct {
	toolMap    map[string]ToolHandler
	resumeChan chan ResumeType
	tracing    *tracingProvider
	events     EventPublisher
}

func newToolExecutor(events EventPublisher, tracing *tracingProvider) *toolExecutor {
	return &toolExecutor{
		toolMap:    make(map[string]ToolHandler),
		resumeChan: make(chan ResumeType),
		tracing:    tracing,
		events:     events,
	}
}

func (t *toolExecutor) RegisterHandler(name string, handler ToolHandler) {
	t.toolMap[name] = handler
}

func (t *toolExecutor) Resume(ctx context.Context, confirmationType ResumeType) {
	slog.Debug("Resuming tool executor", "confirmation_type", confirmationType)

	cType := ResumeTypeApproveSession
	switch confirmationType {
	case ResumeTypeApprove:
		cType = ResumeTypeApprove
	case ResumeTypeReject:
		cType = ResumeTypeReject
	}

	select {
	case t.resumeChan <- cType:
		slog.Debug("Resume signal sent")
	case <-ctx.Done():
		slog.Debug("Resume context cancelled")
	default:
		slog.Debug("Resume channel not ready, ignoring")
	}
}

func (t *toolExecutor) ProcessToolCalls(
	ctx context.Context,
	sess *session.Session,
	calls []tools.ToolCall,
	agentTools []tools.Tool,
	currentAgent *agent.Agent,
	eventsChan chan Event,
) {
	slog.Debug("Processing tool calls", "agent", currentAgent.Name(), "call_count", len(calls))

	for i, toolCall := range calls {
		callCtx, callSpan := t.tracing.StartSpan(ctx, "runtime.tool.call", trace.WithAttributes(
			attribute.String("tool.name", toolCall.Function.Name),
			attribute.String("tool.type", string(toolCall.Type)),
			attribute.String("agent", currentAgent.Name()),
			attribute.String("session.id", sess.ID),
			attribute.String("tool.call_id", toolCall.ID),
		))

		slog.Debug("Processing tool call", "agent", currentAgent.Name(), "tool", toolCall.Function.Name, "session_id", sess.ID)

		def, exists := t.toolMap[toolCall.Function.Name]
		if exists {
			t.handleRegisteredTool(callCtx, sess, toolCall, def, calls, i, currentAgent, eventsChan)
			callSpan.SetStatus(codes.Ok, "tool call processed")
			callSpan.End()
			continue
		}

		t.handleAgentTool(callCtx, sess, toolCall, agentTools, calls, i, currentAgent)

		callSpan.SetStatus(codes.Ok, "tool call processed")
		callSpan.End()
	}
}

func (t *toolExecutor) handleRegisteredTool(
	ctx context.Context,
	sess *session.Session,
	toolCall tools.ToolCall,
	def ToolHandler,
	allCalls []tools.ToolCall,
	currentIndex int,
	currentAgent *agent.Agent,
	eventsChan chan Event,
) {
	slog.Debug("Using runtime tool handler", "tool", toolCall.Function.Name, "session_id", sess.ID)

	if sess.ToolsApproved || def.Tool.Annotations.ReadOnlyHint {
		t.runAgentTool(ctx, def.Handler, sess, toolCall, def.Tool, currentAgent, eventsChan)
		return
	}

	slog.Debug("Tools not approved, waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
	t.events.Publish(ToolCallConfirmation(toolCall, def.Tool, currentAgent.Name()))

	select {
	case cType := <-t.resumeChan:
		switch cType {
		case ResumeTypeApprove:
			slog.Debug("Resume signal received, approving tool handler", "tool", toolCall.Function.Name)
			t.runAgentTool(ctx, def.Handler, sess, toolCall, def.Tool, currentAgent, eventsChan)
		case ResumeTypeApproveSession:
			slog.Debug("Resume signal received, approving session", "tool", toolCall.Function.Name)
			sess.ToolsApproved = true
			t.runAgentTool(ctx, def.Handler, sess, toolCall, def.Tool, currentAgent, eventsChan)
		case ResumeTypeReject:
			slog.Debug("Resume signal received, rejecting tool handler", "tool", toolCall.Function.Name)
			t.addToolRejectedResponse(sess, toolCall, def.Tool, currentAgent)
		}
	case <-ctx.Done():
		slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name)
		t.addToolCancelledResponse(sess, toolCall, def.Tool, currentAgent)
		for j := currentIndex + 1; j < len(allCalls); j++ {
			t.addToolCancelledResponse(sess, allCalls[j], def.Tool, currentAgent)
		}
	}
}

func (t *toolExecutor) handleAgentTool(
	ctx context.Context,
	sess *session.Session,
	toolCall tools.ToolCall,
	agentTools []tools.Tool,
	allCalls []tools.ToolCall,
	currentIndex int,
	currentAgent *agent.Agent,
) {
	for _, tool := range agentTools {
		if _, ok := t.toolMap[tool.Name]; ok {
			continue
		}
		if tool.Name != toolCall.Function.Name {
			continue
		}

		slog.Debug("Using agent tool handler", "tool", toolCall.Function.Name)

		if sess.ToolsApproved || tool.Annotations.ReadOnlyHint {
			slog.Debug("Tools approved, running tool", "tool", toolCall.Function.Name, "session_id", sess.ID)
			t.runTool(ctx, tool, toolCall, sess, currentAgent)
			return
		}

		slog.Debug("Tools not approved, waiting for resume", "tool", toolCall.Function.Name, "session_id", sess.ID)
		t.events.Publish(ToolCallConfirmation(toolCall, tool, currentAgent.Name()))

		select {
		case cType := <-t.resumeChan:
			switch cType {
			case ResumeTypeApprove:
				slog.Debug("Resume signal received, approving tool handler", "tool", toolCall.Function.Name)
				t.runTool(ctx, tool, toolCall, sess, currentAgent)
			case ResumeTypeApproveSession:
				slog.Debug("Resume signal received, approving session", "tool", toolCall.Function.Name)
				sess.ToolsApproved = true
				t.runTool(ctx, tool, toolCall, sess, currentAgent)
			case ResumeTypeReject:
				slog.Debug("Resume signal received, rejecting tool handler", "tool", toolCall.Function.Name)
				t.addToolRejectedResponse(sess, toolCall, tool, currentAgent)
			}
			slog.Debug("Added tool response to session", "tool", toolCall.Function.Name, "session_id", sess.ID)
			return
		case <-ctx.Done():
			slog.Debug("Context cancelled while waiting for resume", "tool", toolCall.Function.Name)
			t.addToolCancelledResponse(sess, toolCall, tool, currentAgent)
			for j := currentIndex + 1; j < len(allCalls); j++ {
				t.addToolCancelledResponse(sess, allCalls[j], tool, currentAgent)
			}
			return
		}
	}
}

func (t *toolExecutor) runTool(
	ctx context.Context,
	tool tools.Tool,
	toolCall tools.ToolCall,
	sess *session.Session,
	currentAgent *agent.Agent,
) {
	ctx, span := t.tracing.StartSpan(ctx, "runtime.tool.handler", trace.WithAttributes(
		attribute.String("tool.name", toolCall.Function.Name),
		attribute.String("agent", currentAgent.Name()),
		attribute.String("session.id", sess.ID),
		attribute.String("tool.call_id", toolCall.ID),
	))
	defer span.End()

	t.events.Publish(ToolCall(toolCall, tool, currentAgent.Name()))

	var res *tools.ToolCallResult
	var err error

	res, err = tool.Handler(ctx, toolCall)

	telemetry.RecordToolCall(ctx, toolCall.Function.Name, sess.ID, currentAgent.Name(), 0, err)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("Tool handler canceled by context", "tool", toolCall.Function.Name)
			res = &tools.ToolCallResult{Output: "The tool call was canceled by the user."}
			span.SetStatus(codes.Ok, "tool handler canceled by user")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "tool handler error")
			slog.Error("Error calling tool", "tool", toolCall.Function.Name, "error", err)
			res = &tools.ToolCallResult{
				Output: fmt.Sprintf("Error calling tool: %v", err),
			}
		}
	} else {
		span.SetStatus(codes.Ok, "tool handler completed")
		slog.Debug("Agent tool call completed", "tool", toolCall.Function.Name, "output_length", len(res.Output))
	}

	t.events.Publish(ToolCallResponse(toolCall, tool, res.Output, currentAgent.Name()))

	content := res.Output
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    content,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(currentAgent, &toolResponseMsg))
}

func (t *toolExecutor) runAgentTool(
	ctx context.Context,
	handler ToolHandlerFunc,
	sess *session.Session,
	toolCall tools.ToolCall,
	tool tools.Tool,
	currentAgent *agent.Agent,
	eventsChan chan Event,
) {
	ctx, span := t.tracing.StartSpan(ctx, "runtime.tool.handler.runtime", trace.WithAttributes(
		attribute.String("tool.name", toolCall.Function.Name),
		attribute.String("agent", currentAgent.Name()),
		attribute.String("session.id", sess.ID),
		attribute.String("tool.call_id", toolCall.ID),
	))
	defer span.End()

	t.events.Publish(ToolCall(toolCall, tool, currentAgent.Name()))

	start := time.Now()
	res, err := handler(ctx, sess, toolCall, eventsChan)
	duration := time.Since(start)

	telemetry.RecordToolCall(ctx, toolCall.Function.Name, sess.ID, currentAgent.Name(), duration, err)

	var output string
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("Runtime tool handler canceled by context", "tool", toolCall.Function.Name)
			output = "The tool call was canceled by the user."
			span.SetStatus(codes.Ok, "runtime tool handler canceled by user")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "runtime tool handler error")
			output = fmt.Sprintf("error calling tool: %v", err)
			slog.Error("Error executing tool", "tool", toolCall.Function.Name, "error", err)
		}
	} else {
		output = res.Output
		span.SetStatus(codes.Ok, "runtime tool handler completed")
		slog.Debug("Tool executed successfully", "tool", toolCall.Function.Name)
	}

	t.events.Publish(ToolCallResponse(toolCall, tool, output, currentAgent.Name()))

	content := output
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    content,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(currentAgent, &toolResponseMsg))
}

func (t *toolExecutor) addToolRejectedResponse(
	sess *session.Session,
	toolCall tools.ToolCall,
	tool tools.Tool,
	currentAgent *agent.Agent,
) {
	result := "The user rejected the tool call."
	t.events.Publish(ToolCallResponse(toolCall, tool, result, currentAgent.Name()))

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(currentAgent, &toolResponseMsg))
}

func (t *toolExecutor) addToolCancelledResponse(
	sess *session.Session,
	toolCall tools.ToolCall,
	tool tools.Tool,
	currentAgent *agent.Agent,
) {
	result := "The tool call was canceled by the user."
	t.events.Publish(ToolCallResponse(toolCall, tool, result, currentAgent.Name()))

	toolResponseMsg := chat.Message{
		Role:       chat.MessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	sess.AddMessage(session.NewAgentMessage(currentAgent, &toolResponseMsg))
}

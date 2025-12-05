package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

type taskTransferHandler struct {
	runtime *LocalRuntime
}

func newTaskTransferHandler(r *LocalRuntime) *taskTransferHandler {
	return &taskTransferHandler{runtime: r}
}

func (h *taskTransferHandler) HandleTaskTransfer(
	ctx context.Context,
	sess *session.Session,
	toolCall tools.ToolCall,
	evts chan Event,
) (*tools.ToolCallResult, error) {
	var params struct {
		Agent          string `json:"agent"`
		Task           string `json:"task"`
		ExpectedOutput string `json:"expected_output"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	a := h.runtime.CurrentAgent()

	ctx, span := h.runtime.tracing.StartSpan(ctx, "runtime.task_transfer", trace.WithAttributes(
		attribute.String("from.agent", a.Name()),
		attribute.String("to.agent", params.Agent),
		attribute.String("session.id", sess.ID),
	))
	defer span.End()

	slog.Debug("Transferring task to agent", "from_agent", a.Name(), "to_agent", params.Agent, "task", params.Task)

	ca := h.runtime.agents.CurrentAgentName()

	evts <- AgentSwitching(true, ca, params.Agent)

	_ = h.runtime.agents.SetCurrentAgent(params.Agent)
	defer func() {
		_ = h.runtime.agents.SetCurrentAgent(ca)

		evts <- AgentSwitching(false, params.Agent, ca)

		if originalAgent, err := h.runtime.agents.Agent(ca); err == nil {
			var modelID string
			if model := originalAgent.Model(); model != nil {
				modelID = model.ID()
			}
			evts <- AgentInfo(originalAgent.Name(), modelID, originalAgent.Description())
		}
	}()

	if newAgent, err := h.runtime.agents.Agent(params.Agent); err == nil {
		var modelID string
		if model := newAgent.Model(); model != nil {
			modelID = model.ID()
		}
		evts <- AgentInfo(newAgent.Name(), modelID, newAgent.Description())
	}

	memberAgentTask := "You are a member of a team of agents. Your goal is to complete the following task:"
	memberAgentTask += fmt.Sprintf("\n\n<task>\n%s\n</task>", params.Task)
	if params.ExpectedOutput != "" {
		memberAgentTask += fmt.Sprintf("\n\n<expected_output>\n%s\n</expected_output>", params.ExpectedOutput)
	}

	slog.Debug("Creating new session with parent session", "parent_session_id", sess.ID, "tools_approved", sess.ToolsApproved)

	child, err := h.runtime.agents.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	s := session.New(
		session.WithSystemMessage(memberAgentTask),
		session.WithImplicitUserMessage("Follow the default instructions"),
		session.WithMaxIterations(child.MaxIterations()),
		session.WithTitle("Transferred task"),
		session.WithToolsApproved(sess.ToolsApproved),
		session.WithSendUserMessage(false),
	)

	for event := range h.runtime.RunStream(ctx, s) {
		evts <- event
		if errEvent, ok := event.(*ErrorEvent); ok {
			span.RecordError(fmt.Errorf("%s", errEvent.Error))
			span.SetStatus(codes.Error, "error in transferred session")
			return nil, fmt.Errorf("%s", errEvent.Error)
		}
	}

	sess.ToolsApproved = s.ToolsApproved

	sess.AddSubSession(s)

	slog.Debug("Task transfer completed", "agent", params.Agent, "task", params.Task)

	span.SetStatus(codes.Ok, "task transfer completed")
	return &tools.ToolCallResult{
		Output: s.GetLastAssistantMessageContent(),
	}, nil
}

func (h *taskTransferHandler) HandleHandoff(
	_ context.Context,
	_ *session.Session,
	toolCall tools.ToolCall,
	_ chan Event,
) (*tools.ToolCallResult, error) {
	var params builtin.HandoffArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	ca := h.runtime.agents.CurrentAgentName()
	next, err := h.runtime.agents.Agent(params.Agent)
	if err != nil {
		return nil, err
	}

	_ = h.runtime.agents.SetCurrentAgent(next.Name())
	return &tools.ToolCallResult{
		Output: fmt.Sprintf("The agent %s handed off the conversation to you, look at the history of the conversation and continue where it left off. Once you are done with your task or if the user asks you, handoff the conversation back to %s.", ca, ca),
	}, nil
}

func (r *LocalRuntime) registerTaskTransferHandlers() {
	handler := newTaskTransferHandler(r)

	tt := builtin.NewTransferTaskTool()
	ht := builtin.NewHandoffTool()
	ttTools, _ := tt.Tools(context.TODO())
	htTools, _ := ht.Tools(context.TODO())

	for _, t := range ttTools {
		r.toolExec.RegisterHandler(t.Name, ToolHandler{
			Handler: handler.HandleTaskTransfer,
			Tool:    t,
		})
	}

	for _, t := range htTools {
		r.toolExec.RegisterHandler(t.Name, ToolHandler{
			Handler: handler.HandleHandoff,
			Tool:    t,
		})
	}

	slog.Debug("Registered task transfer handlers")
}

package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/tools"
)

type elicitationHandler struct {
	requestCh    chan ElicitationResult
	events       EventPublisher
	currentAgent func() string
}

func newElicitationHandler(events EventPublisher, currentAgent func() string) *elicitationHandler {
	return &elicitationHandler{
		requestCh:    make(chan ElicitationResult),
		events:       events,
		currentAgent: currentAgent,
	}
}

func (e *elicitationHandler) Handler(ctx context.Context, req *mcp.ElicitParams) (tools.ElicitationResult, error) {
	slog.Debug("Elicitation request received from MCP server", "message", req.Message)

	if e.events == nil {
		return tools.ElicitationResult{}, fmt.Errorf("no events publisher available for elicitation")
	}

	slog.Debug("Sending elicitation request event to client",
		"message", req.Message,
		"requested_schema", req.RequestedSchema,
	)
	slog.Debug("Elicitation request meta", "meta", req.Meta)

	e.events.Publish(ElicitationRequest(req.Message, req.RequestedSchema, req.Meta, e.currentAgent()))

	select {
	case result := <-e.requestCh:
		slog.Debug("Received elicitation response", "action", result.Action)
		return tools.ElicitationResult{
			Action:  result.Action,
			Content: result.Content,
		}, nil
	case <-ctx.Done():
		slog.Debug("Context cancelled while waiting for elicitation response")
		return tools.ElicitationResult{}, ctx.Err()
	}
}

func (e *elicitationHandler) Resume(ctx context.Context, action tools.ElicitationAction, content map[string]any) error {
	slog.Debug("Resuming with elicitation response", "action", action)

	result := ElicitationResult{
		Action:  action,
		Content: content,
	}

	select {
	case <-ctx.Done():
		slog.Debug("Context cancelled while sending elicitation response")
		return ctx.Err()
	case e.requestCh <- result:
		slog.Debug("Elicitation response sent successfully", "action", action)
		return nil
	default:
		slog.Debug("Elicitation channel not ready")
		return fmt.Errorf("no elicitation request in progress")
	}
}

func (e *elicitationHandler) GetHandlerFunc() func(context.Context, *mcp.ElicitParams) (tools.ElicitationResult, error) {
	return e.Handler
}

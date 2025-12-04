package runtime

import (
	"context"
	"fmt"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

type ResumeType string

const (
	ResumeTypeApprove        ResumeType = "approve"
	ResumeTypeApproveSession ResumeType = "approve-session"
	ResumeTypeReject         ResumeType = "reject"
)

type ToolHandlerFunc func(ctx context.Context, sess *session.Session, toolCall tools.ToolCall, events chan Event) (*tools.ToolCallResult, error)

type ToolHandler struct {
	Handler ToolHandlerFunc
	Tool    tools.Tool
}

type ElicitationResult struct {
	Action  tools.ElicitationAction
	Content map[string]any
}

type ElicitationError struct {
	Action  string
	Message string
}

func (e *ElicitationError) Error() string {
	return fmt.Sprintf("elicitation %s: %s", e.Action, e.Message)
}

type streamResult struct {
	Calls             []tools.ToolCall
	Content           string
	ReasoningContent  string
	ThinkingSignature string
	ThoughtSignature  []byte
	Stopped           bool
}

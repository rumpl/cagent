package runtime

import (
	"context"

	"github.com/docker/docker-agent/pkg/agent"
)

func (r *LocalRuntime) emitError(ctx context.Context, events chan Event, a *agent.Agent, sessionID, message string) {
	if events != nil {
		events <- Error(message)
	}
	if a != nil {
		r.observeNotification(ctx, &ObservedNotification{Runtime: r, Agent: a, SessionID: sessionID, Level: "error", Message: message})
	}
}

func (r *LocalRuntime) emitWarning(ctx context.Context, events chan Event, a *agent.Agent, sessionID, message string) {
	agentName := ""
	if a != nil {
		agentName = a.Name()
	}
	if events != nil {
		events <- Warning(message, agentName)
	}
	if a != nil {
		r.observeNotification(ctx, &ObservedNotification{Runtime: r, Agent: a, SessionID: sessionID, Level: "warning", Message: message})
	}
}

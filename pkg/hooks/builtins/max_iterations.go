package builtins

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/docker/docker-agent/pkg/hooks"
)

// MaxIterations is the registered name of the max_iterations builtin.
const MaxIterations = "max_iterations"

// maxIterations signals a terminating verdict once the runtime's current
// model-call number exceeds the configured limit.
//
// This is a hard stop with no resume protocol — distinct from the
// agent.MaxIterations flag, which has its own special UX
// (MaxIterationsReachedEvent + a resume dialog) and stays in
// pkg/runtime. Use this builtin to express "stop after N model calls,
// period" in YAML.
//
// Args layout: `[limit]`. Missing or invalid args make the hook a
// no-op so a misconfigured YAML doesn't accidentally cap a run at
// zero. The runtime reports the current call number on
// [hooks.Input.ModelCallNumber].
func maxIterations(_ context.Context, in *hooks.Input, args []string) (*hooks.Output, error) {
	if in == nil || in.ModelCallNumber <= 0 || len(args) == 0 {
		return nil, nil
	}
	limit, err := strconv.Atoi(args[0])
	if err != nil || limit <= 0 {
		slog.Debug("max_iterations: ignoring invalid limit", "arg", args[0], "error", err)
		return nil, nil
	}

	if in.ModelCallNumber <= limit {
		return nil, nil
	}

	slog.Warn("max_iterations tripped",
		"model_call_number", in.ModelCallNumber, "limit", limit, "session_id", in.SessionID)

	return &hooks.Output{
		Decision: hooks.DecisionBlockValue,
		Reason: fmt.Sprintf(
			"Agent terminated: max_iterations builtin reached its limit of %d model call(s).",
			limit),
	}, nil
}

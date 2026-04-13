package runtime

import "context"

func (r *LocalRuntime) approvalToolMiddleware() ToolMiddleware {
	return func(ctx context.Context, phase *ToolCallPhase, next ToolHandler) (*ToolExecutionResult, error) {
		if phase == nil {
			return next(ctx, phase)
		}

		var (
			result *ToolExecutionResult
			err    error
		)
		canceled := r.executeWithApproval(
			ctx,
			phase.Execution,
			phase.Session,
			phase.ToolCall,
			phase.Tool,
			phase.Events,
			phase.Agent,
			func() {
				result, err = next(ctx, phase)
			},
		)
		if canceled {
			return &ToolExecutionResult{Canceled: true}, nil
		}
		if result == nil {
			return &ToolExecutionResult{}, err
		}
		return result, err
	}
}

func (r *LocalRuntime) shellHookToolMiddleware() ToolMiddleware {
	return func(ctx context.Context, phase *ToolCallPhase, next ToolHandler) (*ToolExecutionResult, error) {
		if phase == nil || phase.Agent == nil || phase.Session == nil {
			return next(ctx, phase)
		}

		if _, exists := r.toolMap[phase.ToolCall.Function.Name]; exists {
			return next(ctx, phase)
		}

		hooksExec := r.getHooksExecutor(phase.Agent)
		if hooksExec == nil {
			return next(ctx, phase)
		}

		if hooksExec.HasPreToolUseHooks() {
			blocked, modifiedTC := r.executePreToolHook(ctx, hooksExec, phase.Session, phase.ToolCall, phase.Tool, phase.Events, phase.Agent)
			if blocked {
				return &ToolExecutionResult{}, nil
			}
			phase.ToolCall = modifiedTC
		}

		result, err := next(ctx, phase)
		if err != nil {
			return result, err
		}
		if result != nil && result.Canceled {
			return result, nil
		}

		if hooksExec.HasPostToolUseHooks() {
			postResult := r.executePostToolHook(ctx, hooksExec, phase.Session, phase.ToolCall, phase.Events, phase.Agent)
			if postResult != nil {
				appendExecutionPromptContext(phase.Execution, postResult.AdditionalContext)
			}
		}

		return result, nil
	}
}

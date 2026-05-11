package cloudbridge

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// SubmitFunc is invoked once per pulled prompt. Implementations are expected
// to enqueue the prompt onto the local runtime addressing the session
// identified by externalID and to return promptly.
//
// Note: the server claims prompts atomically (delete-on-return) from
// session_prompt_queue, so there is no separate ack step — a successful Pull
// response constitutes the only confirmation. If the client crashes before
// processing a delivered prompt, that prompt is lost (at-most-once).
type SubmitFunc func(externalID, prompt, sendID string) error

// AP serves Connect unary JSON with proto3-canonical (camelCase) field names.
// All on-wire types in this package use camelCase to match.

// pullRequest matches LocalAgentService.PullLocalPrompts.
type pullRequest struct {
	WaitSeconds int `json:"waitSeconds"`
}

// pullResponse matches LocalAgentService.PullLocalPrompts response.
type pullResponse struct {
	Prompts []remotePrompt `json:"prompts"`
}

type remotePrompt struct {
	ExternalID string `json:"externalId"`
	Prompt     string `json:"prompt"`
	SendID     string `json:"sendId"`
}

const (
	pullerWaitSeconds = 30
	pullerMinBackoff  = time.Second
	pullerMaxBackoff  = 30 * time.Second
)

// StartPuller spawns a goroutine that long-polls AP for remote prompts
// addressed to this host and dispatches them via submitFn.
//
// The goroutine runs until ctx is cancelled. Errors are logged and retried
// with exponential backoff (1s → 30s).
//
// Returns immediately. Returns an error only for setup problems (e.g.
// missing credentials when the puller is started).
func StartPuller(ctx context.Context, submitFn SubmitFunc) error {
	if submitFn == nil {
		submitFn = dispatchPrompt
	}
	go runPuller(ctx, submitFn)
	return nil
}

func runPuller(ctx context.Context, submitFn SubmitFunc) {
	backoff := pullerMinBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := pullOnce(ctx, submitFn)
		if err == nil {
			backoff = pullerMinBackoff
			continue
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Debug("cloudbridge: puller error", "error", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > pullerMaxBackoff {
			backoff = pullerMaxBackoff
		}
	}
}

func pullOnce(ctx context.Context, submitFn SubmitFunc) error {
	// Long-poll: budget is wait_seconds + a margin for response time.
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(pullerWaitSeconds+15)*time.Second)
	defer cancel()

	var resp pullResponse
	if err := CallAP(callCtx, endpoint(), "/api/platform.v1.LocalAgentService/PullLocalPrompts",
		pullRequest{WaitSeconds: pullerWaitSeconds}, &resp); err != nil {
		return err
	}

	if len(resp.Prompts) == 0 {
		return nil
	}

	for _, p := range resp.Prompts {
		slog.Info("cloudbridge: received remote prompt",
			"external_id", p.ExternalID, "send_id", p.SendID)
		if err := submitFn(p.ExternalID, p.Prompt, p.SendID); err != nil {
			slog.Warn("cloudbridge: submit handler returned error",
				"external_id", p.ExternalID, "send_id", p.SendID, "error", err)
		}
	}
	return nil
}

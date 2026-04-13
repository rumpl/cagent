package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelsdev"
)

// SummaryService produces a summary message from a provider without going
// through the general runtime loop.
type SummaryService interface {
	SummarizeMessages(ctx context.Context, model provider.Provider, definition *modelsdev.Model, messages []chat.Message) (*SummaryResult, error)
}

// SummaryResult contains the generated summary text and any usage/cost data
// reported by the provider.
type SummaryResult struct {
	Content string
	Usage   *chat.Usage
	Cost    float64
}

type modelSummaryService struct{}

func (modelSummaryService) SummarizeMessages(ctx context.Context, model provider.Provider, definition *modelsdev.Model, messages []chat.Message) (*SummaryResult, error) {
	stream, err := model.CreateChatCompletionStream(ctx, messages, nil)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	var (
		content strings.Builder
		usage   *chat.Usage
	)

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error receiving summary stream: %w", err)
		}
		if response.Usage != nil {
			usage = response.Usage
		}
		if len(response.Choices) == 0 {
			continue
		}

		choice := response.Choices[0]
		if choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
		}
		if choice.FinishReason == chat.FinishReasonStop || choice.FinishReason == chat.FinishReasonLength {
			break
		}
	}

	return &SummaryResult{
		Content: content.String(),
		Usage:   usage,
		Cost:    estimateUsageCost(usage, definition),
	}, nil
}

func estimateUsageCost(usage *chat.Usage, model *modelsdev.Model) float64 {
	if usage == nil || model == nil || model.Cost == nil {
		return 0
	}
	return (float64(usage.InputTokens)*model.Cost.Input +
		float64(usage.OutputTokens)*model.Cost.Output +
		float64(usage.CachedInputTokens)*model.Cost.CacheRead +
		float64(usage.CacheWriteTokens)*model.Cost.CacheWrite) / 1e6
}

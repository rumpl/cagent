package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const compactionSystemPrompt = "You are a helpful AI assistant that creates comprehensive summaries of conversations. You will be given a conversation history and asked to create a concise yet thorough summary that captures the key points, decisions made, and outcomes."

const compactionUserPromptFormat = `Based on the following conversation between a user and an AI assistant, create a comprehensive summary that captures:
- The main topics discussed
- Key information exchanged
- Decisions made or conclusions reached
- Important outcomes or results

Provide a well-structured summary (2-4 paragraphs) that someone could read to understand what happened in this conversation. Return ONLY the summary text, nothing else.

Conversation history:%s

Generate a summary for this conversation:`

type compactor struct {
	wg           sync.WaitGroup
	model        provider.Provider
	sessionStore session.Store
}

func newCompactor(model provider.Provider, sessionStore session.Store) *compactor {
	return &compactor{
		model:        model,
		sessionStore: sessionStore,
	}
}

func (c *compactor) Compact(ctx context.Context, sess *session.Session, additionalPrompt, agentName string, events chan<- Event) {
	c.wg.Go(func() {
		c.compact(ctx, sess, additionalPrompt, agentName, events)
	})
}

func (c *compactor) CompactSync(ctx context.Context, sess *session.Session, additionalPrompt, agentName string, events chan<- Event) {
	c.compact(ctx, sess, additionalPrompt, agentName, events)
}

func (c *compactor) Wait() {
	c.wg.Wait()
}

func (c *compactor) compact(ctx context.Context, sess *session.Session, additionalPrompt, agentName string, events chan<- Event) {
	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", agentName)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", agentName)
	}()

	messages := sess.GetAllMessages()
	if len(messages) == 0 {
		events <- Warning("Session is empty. Start a conversation before compacting.", agentName)
		return
	}

	conversationHistory := c.buildConversationHistory(messages)
	userPrompt := c.buildUserPrompt(conversationHistory, additionalPrompt)

	summary, err := c.generateSummary(ctx, userPrompt)
	if err != nil {
		slog.Error("Failed to generate session summary", "session_id", sess.ID, "error", err)
		return
	}

	if summary == "" {
		return
	}

	sess.Messages = append(sess.Messages, session.Item{Summary: summary})
	_ = c.sessionStore.UpdateSession(ctx, sess)
	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, agentName)
}

func (c *compactor) buildConversationHistory(messages []session.Message) string {
	var conversationHistory strings.Builder
	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			continue
		}
		fmt.Fprintf(&conversationHistory, "\n%s: %s", role, messages[i].Message.Content)
	}
	return conversationHistory.String()
}

func (c *compactor) buildUserPrompt(conversationHistory, additionalPrompt string) string {
	userPrompt := fmt.Sprintf(compactionUserPromptFormat, conversationHistory)
	if additionalPrompt != "" {
		userPrompt += fmt.Sprintf("\n\nAdditional instructions from user: %s", additionalPrompt)
	}
	return userPrompt
}

func (c *compactor) generateSummary(ctx context.Context, userPrompt string) (string, error) {
	newModel := provider.CloneWithOptions(ctx, c.model, options.WithStructuredOutput(nil))
	newTeam := team.New(
		team.WithAgents(agent.New("root", compactionSystemPrompt, agent.WithModel(newModel))),
	)

	summarySession := session.New(session.WithSystemMessage(compactionSystemPrompt))
	summarySession.AddMessage(session.UserMessage(userPrompt))
	summarySession.Title = "Generating summary..."

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		return "", fmt.Errorf("failed to create summary generator runtime: %w", err)
	}

	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		return "", fmt.Errorf("failed to run summary generation: %w", err)
	}

	return summarySession.GetLastAssistantMessageContent(), nil
}

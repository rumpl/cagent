package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

type Compaction struct {
	llm provider.Provider
}

func NewCompaction(agents *team.Team) (*Compaction, error) {
	var model provider.Provider

	root, err := agents.Agent("root")
	if err != nil {
		agentNames := agents.AgentNames()
		for _, name := range agentNames {
			a, err := agents.Agent(name)
			if err != nil {
				continue
			}
			model = provider.CloneWithOptions(context.TODO(), a.Model(), options.WithStructuredOutput(nil))
			break
		}
	} else {
		model = provider.CloneWithOptions(context.TODO(), root.Model(), options.WithStructuredOutput(nil))
	}
	if model == nil {
		return nil, errors.New("no models available for compaction")
	}
	return &Compaction{llm: model}, nil
}

// Summarize generates a summary for the session based on the conversation history
func (r *Compaction) Summarize(ctx context.Context, sess *session.Session, currentAgent string, events chan Event) {
	messages := sess.GetAllMessages()
	if len(messages) == 0 {
		events <- &WarningEvent{Message: "Session is empty. Start a conversation before compacting."}
		return
	}

	slog.Debug("Generating summary for session", "session_id", sess.ID)

	events <- SessionCompaction(sess.ID, "started", currentAgent)
	defer func() {
		events <- SessionCompaction(sess.ID, "completed", currentAgent)
	}()

	var conversationHistory strings.Builder

	for i := range messages {
		role := "Unknown"
		switch messages[i].Message.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			continue // Skip system messages for summarization
		}
		conversationHistory.WriteString(fmt.Sprintf("\n%s: %s", role, messages[i].Message.Content))
	}

	// Create a new session for summary generation
	systemPrompt := "You are a helpful AI assistant that creates comprehensive summaries of conversations. You will be given a conversation history and asked to create a concise yet thorough summary that captures the key points, decisions made, and outcomes."
	userPrompt := fmt.Sprintf("Based on the following conversation between a user and an AI assistant, create a comprehensive summary that captures:\n- The main topics discussed\n- Key information exchanged\n- Decisions made or conclusions reached\n- Important outcomes or results\n\nProvide a well-structured summary (2-4 paragraphs) that someone could read to understand what happened in this conversation. Return ONLY the summary text, nothing else.\n\nConversation history:%s\n\nGenerate a summary for this conversation:", conversationHistory.String())
	newTeam := team.New(
		team.WithAgents(agent.New("root", systemPrompt, agent.WithModel(r.llm))),
	)

	summarySession := session.New(session.WithSystemMessage(systemPrompt))
	summarySession.AddMessage(session.UserMessage(userPrompt))
	summarySession.Title = "Generating summary..."

	summaryRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create summary generator runtime", "error", err)
		return
	}

	// Run the summary generation
	_, err = summaryRuntime.Run(ctx, summarySession)
	if err != nil {
		slog.Error("Failed to generate session summary", "session_id", sess.ID, "error", err)
		return
	}

	summary := summarySession.GetLastAssistantMessageContent()
	if summary == "" {
		return
	}
	// Add the summary to the session as a summary item
	sess.Messages = append(sess.Messages, session.Item{Summary: summary})
	slog.Debug("Generated session summary", "session_id", sess.ID, "summary_length", len(summary))
	events <- SessionSummary(sess.ID, summary, currentAgent)
}

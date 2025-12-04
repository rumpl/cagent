package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

const (
	defaultMaxTitleLength = 50
	titleSystemPrompt     = "You are a helpful AI assistant that generates concise, descriptive titles for conversations. You will be given a conversation history and asked to create a title that captures the main topic."
	titleUserPromptFormat = "Based on the following message a user sent to an AI assistant, generate a short, descriptive title (maximum 50 characters) that captures the main topic or purpose of the conversation. Return ONLY the title text, nothing else.\n\nUser message: %s\n\n"
)

type channelPublisher struct {
	ch chan Event
}

func (p *channelPublisher) Publish(event Event) {
	p.ch <- event
}

type titleGenerator struct {
	wg           sync.WaitGroup
	events       EventPublisher
	getModel     func() provider.Provider
	currentAgent func() string
}

func newTitleGenerator(events EventPublisher, getModel func() provider.Provider, currentAgent func() string) *titleGenerator {
	return &titleGenerator{
		events:       events,
		getModel:     getModel,
		currentAgent: currentAgent,
	}
}

func (t *titleGenerator) Generate(ctx context.Context, sess *session.Session) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.generate(ctx, sess)
	}()
}

func (t *titleGenerator) Wait() {
	t.wg.Wait()
}

func (t *titleGenerator) generate(ctx context.Context, sess *session.Session) {
	slog.Debug("Generating title for session", "session_id", sess.ID)

	firstUserMessage := sess.GetLastUserMessageContent()
	if firstUserMessage == "" {
		slog.Error("Failed generating session title: no user message found in session", "session_id", sess.ID)
		t.events.Publish(SessionTitle(sess.ID, "Untitled", t.currentAgent()))
		return
	}

	userPrompt := fmt.Sprintf(titleUserPromptFormat, firstUserMessage)

	titleModel := provider.CloneWithOptions(
		ctx,
		t.getModel(),
		options.WithStructuredOutput(nil),
		options.WithMaxTokens(100),
		options.WithGeneratingTitle(),
	)

	newTeam := team.New(
		team.WithAgents(agent.New("root", titleSystemPrompt, agent.WithModel(titleModel))),
	)

	titleSession := session.New(
		session.WithUserMessage(userPrompt),
		session.WithTitle("Generating title..."),
	)

	titleRuntime, err := New(newTeam, WithSessionCompaction(false))
	if err != nil {
		slog.Error("Failed to create title generator runtime", "error", err)
		return
	}

	_, err = titleRuntime.Run(ctx, titleSession)
	if err != nil {
		slog.Error("Failed to generate session title", "session_id", sess.ID, "error", err)
		return
	}

	title := titleSession.GetLastAssistantMessageContent()
	if title == "" {
		return
	}

	title = TruncateTitle(title, defaultMaxTitleLength)
	sess.Title = title
	slog.Debug("Generated session title", "session_id", sess.ID, "title", title)
	t.events.Publish(SessionTitle(sess.ID, title, t.currentAgent()))
}

func TruncateTitle(title string, maxLength int) string {
	if len(title) <= maxLength {
		return title
	}
	if maxLength < 3 {
		return "..."
	}
	return title[:maxLength-3] + "..."
}

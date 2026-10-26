package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
)

func TestSyntheticAssistantAnnouncesTextBeforeCanonicalMessage(t *testing.T) {
	t.Parallel()
	sess := session.New()
	a := agent.New("root", "test")
	var events []contract.Event
	msg := &chat.Message{Role: chat.MessageRoleAssistant, Content: "FINAL-ANSWER"}
	addAgentMessage(sess, a, msg, EventSinkFunc(func(e contract.Event) { events = append(events, e) }))
	require.Len(t, events, 2)
	choice := events[0].(*contract.AgentChoiceEvent)
	require.Equal(t, "FINAL-ANSWER", choice.Content)
	require.NotEmpty(t, choice.MessageID)
	added := events[1].(*contract.MessageAddedEvent)
	require.Equal(t, choice.MessageID, added.Message.Message.MessageID)
}

func TestAlreadyStreamedAssistantDoesNotAnnounceTextTwice(t *testing.T) {
	t.Parallel()
	sess := session.New()
	var events []contract.Event
	msg := &chat.Message{Role: chat.MessageRoleAssistant, MessageID: "streamed", Content: "FINAL-ANSWER"}
	addAgentMessage(sess, agent.New("root", "test"), msg, EventSinkFunc(func(e contract.Event) { events = append(events, e) }))
	require.Len(t, events, 1)
	require.IsType(t, &contract.MessageAddedEvent{}, events[0])
}

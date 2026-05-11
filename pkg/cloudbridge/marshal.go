package cloudbridge

import (
	"encoding/json"

	"github.com/docker/docker-agent/pkg/session"
)

// marshalChatMessage serializes the chat.Message payload of a session.Message
// using docker-agent's native JSON shape — the same encoding the backend's
// session_items.message_json column stores.
//
// agentName / implicit travel as separate fields on the AP request, so they
// are intentionally not part of this payload.
func marshalChatMessage(msg *session.Message) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}
	return json.Marshal(msg.Message)
}

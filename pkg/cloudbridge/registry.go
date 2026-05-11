package cloudbridge

import (
	"fmt"
	"log/slog"
	"sync"
)

// PromptHandler is invoked when a remote prompt arrives for the registered
// session. Implementations are expected to inject the prompt into the local
// runtime (e.g. by emitting a SendMsg into the TUI's event bus) and return
// promptly — long-running work must run on a background goroutine.
type PromptHandler func(prompt, sendID string)

var (
	handlersMu sync.RWMutex
	handlers   = make(map[string]PromptHandler)
)

// RegisterPromptHandler associates a handler with an external session ID.
// Replacing an existing handler is allowed; the most recent registration wins.
// Pair every call with a [UnregisterPromptHandler] when the session closes.
func RegisterPromptHandler(externalID string, h PromptHandler) {
	if externalID == "" || h == nil {
		return
	}
	handlersMu.Lock()
	defer handlersMu.Unlock()
	handlers[externalID] = h
}

// UnregisterPromptHandler removes the handler for externalID, if any.
func UnregisterPromptHandler(externalID string) {
	if externalID == "" {
		return
	}
	handlersMu.Lock()
	defer handlersMu.Unlock()
	delete(handlers, externalID)
}

// dispatchPrompt looks up the handler for externalID and invokes it. Used by
// the long-poller as the default submit path when no SubmitFunc is supplied.
func dispatchPrompt(externalID, prompt, sendID string) error {
	handlersMu.RLock()
	h := handlers[externalID]
	handlersMu.RUnlock()
	if h == nil {
		slog.Warn("cloudbridge: remote prompt for unknown session",
			"external_id", externalID, "send_id", sendID)
		return fmt.Errorf("no prompt handler registered for session %s", externalID)
	}
	h(prompt, sendID)
	return nil
}

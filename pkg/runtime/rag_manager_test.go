package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/team"
)

type ragTestPublisher struct {
	events []Event
}

func (m *ragTestPublisher) Publish(event Event) {
	m.events = append(m.events, event)
}

func TestRuntimeRAGManager_StartBackgroundInit_NoManagers(t *testing.T) {
	publisher := &ragTestPublisher{}
	tm := team.New()

	mgr := newRuntimeRAGManager(
		tm,
		publisher,
		func() string { return "test-agent" },
	)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	mgr.StartBackgroundInit(ctx)

	assert.True(t, mgr.IsInitialized(), "expected initialized to be true")
}

func TestRuntimeRAGManager_StartBackgroundInit_Idempotent(t *testing.T) {
	publisher := &ragTestPublisher{}
	tm := team.New()

	mgr := newRuntimeRAGManager(
		tm,
		publisher,
		func() string { return "test-agent" },
	)

	ctx := t.Context()

	mgr.StartBackgroundInit(ctx)
	mgr.StartBackgroundInit(ctx)
	mgr.StartBackgroundInit(ctx)

	assert.True(t, mgr.IsInitialized(), "expected initialized to be true")
}

func TestRuntimeRAGManager_Initialize_SkipsIfAlreadyDone(t *testing.T) {
	publisher := &ragTestPublisher{}
	tm := team.New()

	mgr := newRuntimeRAGManager(
		tm,
		publisher,
		func() string { return "test-agent" },
	)

	ctx := t.Context()

	mgr.StartBackgroundInit(ctx)

	mgr.Initialize(ctx)

	assert.True(t, mgr.IsInitialized(), "expected initialized to be true")
}

func TestRuntimeRAGManager_IsInitialized(t *testing.T) {
	publisher := &ragTestPublisher{}
	tm := team.New()

	mgr := newRuntimeRAGManager(
		tm,
		publisher,
		func() string { return "test-agent" },
	)

	assert.False(t, mgr.IsInitialized(), "expected not initialized initially")

	mgr.StartBackgroundInit(t.Context())

	assert.True(t, mgr.IsInitialized(), "expected initialized after StartBackgroundInit")
}

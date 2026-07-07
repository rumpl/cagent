package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsageTrackerEmptyActiveAndRoot(t *testing.T) {
	t.Parallel()

	u := NewUsageTracker()

	assert.True(t, u.Empty())
	_, ok := u.Active()
	assert.False(t, ok)
	assert.Empty(t, u.RootSessionID())
	assert.Zero(t, u.TotalCost())
}

func TestUsageTrackerRecordAdoptsRootAndTotalsCost(t *testing.T) {
	t.Parallel()

	u := NewUsageTracker()
	u.Record("root", UsageSnapshot{ContextLength: 10, ContextLimit: 100, Tokens: 50, Cost: 0.25})
	u.Record("child", UsageSnapshot{ContextLength: 5, ContextLimit: 50, Tokens: 20, Cost: 0.75})

	active, ok := u.Active()
	assert.True(t, ok)
	assert.EqualValues(t, 10, active.ContextLength)
	assert.Equal(t, "root", u.RootSessionID())
	assert.InDelta(t, 1.0, u.TotalCost(), 0.0001)
	assert.False(t, u.Empty())
}

func TestUsageTrackerStackControlsActiveSession(t *testing.T) {
	t.Parallel()

	u := NewUsageTracker()
	u.StreamStarted("root")
	u.Record("root", UsageSnapshot{Tokens: 1})
	u.StreamStarted("child")
	u.Record("child", UsageSnapshot{Tokens: 2})

	active, ok := u.Active()
	assert.True(t, ok)
	assert.EqualValues(t, 2, active.Tokens)
	assert.Equal(t, "root", u.RootSessionID())

	u.StreamStopped()
	active, ok = u.Active()
	assert.True(t, ok)
	assert.EqualValues(t, 1, active.Tokens)

	u.StreamStopped()
	u.StreamStopped()
	active, ok = u.Active()
	assert.True(t, ok)
	assert.EqualValues(t, 1, active.Tokens)
}

func TestUsageTrackerIgnoresEmptyStreamIDAndReset(t *testing.T) {
	t.Parallel()

	u := NewUsageTracker()
	u.StreamStarted("")
	u.Record("only", UsageSnapshot{Tokens: 3})
	u.Reset()

	assert.True(t, u.Empty())
	assert.Empty(t, u.RootSessionID())
	_, ok := u.Active()
	assert.False(t, ok)
}

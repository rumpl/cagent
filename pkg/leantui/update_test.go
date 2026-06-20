package leantui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/config/types"
	"github.com/docker/docker-agent/pkg/effort"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/skills"
	"github.com/docker/docker-agent/pkg/tui/messages"
)

type cycleThinkingApp struct {
	supports bool
	level    effort.Level
	err      error
	calls    int
}

func (a *cycleThinkingApp) SupportsModelSwitching() bool { return a.supports }

func (a *cycleThinkingApp) CycleAgentThinkingLevel(context.Context) (effort.Level, error) {
	a.calls++
	return a.level, a.err
}

func (a *cycleThinkingApp) SubscribeWith(context.Context, func(tea.Msg)) {}
func (a *cycleThinkingApp) IsReadOnly() bool                             { return false }
func (a *cycleThinkingApp) NewSession()                                  {}
func (a *cycleThinkingApp) SkillCommandFork(context.Context, string) (string, string, bool) {
	return "", "", false
}
func (a *cycleThinkingApp) LookupCommand(context.Context, string) (types.Command, string, bool) {
	return types.Command{}, "", false
}
func (a *cycleThinkingApp) ResolveInput(context.Context, string) string { return "" }
func (a *cycleThinkingApp) ResolveSkillCommand(context.Context, string) (string, error) {
	return "", nil
}
func (a *cycleThinkingApp) Run(context.Context, context.CancelFunc, string, []messages.Attachment) {
}
func (a *cycleThinkingApp) CompactSession(context.Context, context.CancelFunc, string) {}
func (a *cycleThinkingApp) RunSkillFork(context.Context, context.CancelFunc, string, string, []messages.Attachment) {
}
func (a *cycleThinkingApp) ShouldExitAfterFirstResponse() bool                  { return false }
func (a *cycleThinkingApp) CurrentAgentCommands(context.Context) types.Commands { return nil }
func (a *cycleThinkingApp) CurrentAgentSkills() []skills.Skill                  { return nil }
func (a *cycleThinkingApp) Resume(runtime.ResumeRequest)                        {}

func TestHandleCycleThinkingLevel(t *testing.T) {
	t.Parallel()

	t.Run("updates status when cycling succeeds", func(t *testing.T) {
		t.Parallel()

		m := &model{app: &cycleThinkingApp{supports: true, level: effort.High}}

		m.handleCycleThinkingLevel(t.Context())

		assert.Equal(t, "high", m.status.thinking)
		assert.Equal(t, 1, m.app.(*cycleThinkingApp).calls)
	})

	t.Run("renders none as off", func(t *testing.T) {
		t.Parallel()

		m := &model{app: &cycleThinkingApp{supports: true, level: effort.None}}

		m.handleCycleThinkingLevel(t.Context())

		assert.Equal(t, "off", m.status.thinking)
	})

	t.Run("does not call runtime when model switching is unsupported", func(t *testing.T) {
		t.Parallel()

		m := &model{app: &cycleThinkingApp{supports: false, level: effort.High}}

		m.handleCycleThinkingLevel(t.Context())

		assert.Empty(t, m.status.thinking)
		assert.Equal(t, 0, m.app.(*cycleThinkingApp).calls)
		assert.Len(t, m.blocks, 1)
	})

	t.Run("reports unsupported models", func(t *testing.T) {
		t.Parallel()

		m := &model{app: &cycleThinkingApp{supports: true, err: runtime.ErrUnsupported}}

		m.handleCycleThinkingLevel(t.Context())

		assert.Empty(t, m.status.thinking)
		assert.Len(t, m.blocks, 1)
	})

	t.Run("reports unexpected errors", func(t *testing.T) {
		t.Parallel()

		m := &model{app: &cycleThinkingApp{supports: true, err: errors.New("boom")}}

		m.handleCycleThinkingLevel(t.Context())

		assert.Empty(t, m.status.thinking)
		assert.Len(t, m.blocks, 1)
	})
}

func TestHandleKeyShiftTabCyclesThinkingLevel(t *testing.T) {
	t.Parallel()

	m := &model{
		app:    &cycleThinkingApp{supports: true, level: effort.Medium},
		editor: newEditor(""),
		ac:     newAutocomplete(),
	}

	m.handleKey(t.Context(), key{typ: keyShiftTab})

	assert.Equal(t, "medium", m.status.thinking)
	assert.Equal(t, 1, m.app.(*cycleThinkingApp).calls)
}

var _ leanApp = (*cycleThinkingApp)(nil)

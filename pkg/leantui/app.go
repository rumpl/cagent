package leantui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/config/types"
	"github.com/docker/docker-agent/pkg/effort"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/skills"
	"github.com/docker/docker-agent/pkg/tui/messages"
)

type leanApp interface {
	SubscribeWith(context.Context, func(tea.Msg))
	IsReadOnly() bool
	NewSession()
	SkillCommandFork(context.Context, string) (string, string, bool)
	LookupCommand(context.Context, string) (types.Command, string, bool)
	ResolveInput(context.Context, string) string
	ResolveSkillCommand(context.Context, string) (string, error)
	Run(context.Context, context.CancelFunc, string, []messages.Attachment)
	CompactSession(context.Context, context.CancelFunc, string)
	RunSkillFork(context.Context, context.CancelFunc, string, string, []messages.Attachment)
	ShouldExitAfterFirstResponse() bool
	CurrentAgentCommands(context.Context) types.Commands
	CurrentAgentSkills() []skills.Skill
	Resume(runtime.ResumeRequest)
	SupportsModelSwitching() bool
	CycleAgentThinkingLevel(context.Context) (effort.Level, error)
}

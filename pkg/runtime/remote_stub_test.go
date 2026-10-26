package runtime

import (
	"context"
	"fmt"

	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// stubRemoteClient is a RemoteClient that returns enough state for the
// non-streaming Runtime contract to exercise RemoteRuntime end-to-end.
//
// Methods used only by streaming paths (RunAgent, RunAgentWithAgentName,
// CreateSession) panic so an accidental wiring against them is loud
// rather than silent.
type stubRemoteClient struct {
	cfg      *latest.Config
	getAgent func(context.Context, string) (*latest.Config, error)
}

func (s *stubRemoteClient) GetAgent(ctx context.Context, id string) (*latest.Config, error) {
	if s.getAgent != nil {
		return s.getAgent(ctx, id)
	}
	return s.cfg, nil
}

func (s *stubRemoteClient) CreateSession(context.Context, *session.Session) (*session.Session, error) {
	panic("CreateSession not exercised by the contract test")
}

func (s *stubRemoteClient) ResumeSession(context.Context, string, string, string, string) error {
	return nil
}

func (s *stubRemoteClient) ResumeElicitation(context.Context, string, tools.ElicitationAction, map[string]any, ...string) error {
	return nil
}

func (s *stubRemoteClient) RunAgent(context.Context, string, string, []api.Message, string) (<-chan contract.Event, error) {
	panic("RunAgent not exercised by the contract test")
}

func (s *stubRemoteClient) RunAgentWithAgentName(context.Context, string, string, string, []api.Message, string) (<-chan contract.Event, error) {
	panic("RunAgentWithAgentName not exercised by the contract test")
}

func (s *stubRemoteClient) SteerSession(context.Context, string, []api.Message) error {
	return nil
}

func (s *stubRemoteClient) FollowUpSession(context.Context, string, []api.Message) error {
	return nil
}

func (s *stubRemoteClient) UpdateSessionTitle(context.Context, string, string) error {
	return nil
}

func (s *stubRemoteClient) GetAgentToolCount(context.Context, string, string) (int, error) {
	return 0, nil
}

func (s *stubRemoteClient) StreamSessionEvents(context.Context, string) (<-chan contract.Event, error) {
	panic("StreamSessionEvents not exercised by the contract test")
}

func (s *stubRemoteClient) GetAllSessions(context.Context) ([]session.Session, error) {
	return nil, nil
}

func (s *stubRemoteClient) DeleteRemoteSession(context.Context, string) error {
	return nil
}

func (s *stubRemoteClient) GetSessionTools(context.Context, string) ([]tools.Tool, error) {
	return nil, nil
}

func (s *stubRemoteClient) GetAvailableModels(context.Context) ([]string, error) {
	return nil, nil
}

func (s *stubRemoteClient) GetSessionMCPPrompts(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (s *stubRemoteClient) ExecuteSessionMCPPrompt(_ context.Context, _, promptName string, _ map[string]string) (string, error) {
	return "", fmt.Errorf("prompt %q not found", promptName)
}

func (s *stubRemoteClient) GetSessionSkills(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (s *stubRemoteClient) CompactSession(context.Context, string) error {
	return nil
}

func (s *stubRemoteClient) GetSessionToolsets(context.Context, string) ([]map[string]any, error) {
	return nil, nil
}

func (s *stubRemoteClient) RestartSessionToolset(_ context.Context, _, name string) error {
	return fmt.Errorf("toolset %q not found", name)
}

func (s *stubRemoteClient) PauseSession(context.Context, string) error {
	return nil
}

func (s *stubRemoteClient) GetSessionSnapshots(context.Context, string) ([]map[string]any, error) {
	return nil, nil
}

func (s *stubRemoteClient) UndoSession(context.Context, string) error {
	return nil
}

func (s *stubRemoteClient) ResetSession(context.Context, string) error {
	return nil
}

func (s *stubRemoteClient) AddMessage(context.Context, string, *session.Message) error {
	return nil
}

func (s *stubRemoteClient) UpdateMessage(context.Context, string, string, *session.Message) error {
	return nil
}

func (s *stubRemoteClient) AddSummary(context.Context, string, session.Item) error {
	return nil
}

func (s *stubRemoteClient) UpdateSessionTokens(context.Context, string, int64, int64, float64) error {
	return nil
}

func (s *stubRemoteClient) SetSessionStarred(context.Context, string, bool) error {
	return nil
}

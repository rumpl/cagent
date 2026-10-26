package runtime

import "github.com/docker/docker-agent/pkg/runtime/remote"

// RemoteRuntime executes agents through a remote server.
type RemoteRuntime = remote.Runtime

// RemoteRuntimeOption configures a remote runtime.
type RemoteRuntimeOption = remote.Option

// RemoteSessionStore browses remote session history.
type RemoteSessionStore = remote.SessionStore

func NewRemoteRuntime(c RemoteClient, opts ...RemoteRuntimeOption) (*RemoteRuntime, error) {
	return remote.NewRuntime(c, opts...)
}

func WithRemoteCurrentAgent(name string) RemoteRuntimeOption {
	return remote.WithRemoteCurrentAgent(name)
}

func WithRemoteAgentFilename(name string) RemoteRuntimeOption {
	return remote.WithRemoteAgentFilename(name)
}

func NewRemoteSessionStore(c RemoteClient) *RemoteSessionStore {
	return remote.NewSessionStore(c)
}

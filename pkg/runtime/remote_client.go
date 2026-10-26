package runtime

import "github.com/docker/docker-agent/pkg/runtime/remote"

// RemoteClient is the transport contract for remote execution.
type RemoteClient = remote.Backend

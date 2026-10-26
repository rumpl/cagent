package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime/contract"
	"github.com/docker/docker-agent/pkg/session"
)

func TestOperationIdentity(t *testing.T) {
	t.Parallel()
	a := New(t.Context(), &mockRuntime{}, session.New())
	first := a.BeginOperation(t.Context())
	stop := contract.StreamStopped(a.session.ID, "root", "canceled")
	tagged := operationEvent(context.WithoutCancel(first), stop)
	assert.True(t, a.IsCurrentOperation(tagged))
	second := a.BeginOperation(t.Context())
	assert.False(t, a.IsCurrentOperation(tagged))
	assert.True(t, a.IsCurrentOperation(operationEvent(second, contract.StreamStarted(a.session.ID, "root"))))
	assert.Zero(t, stop.(*contract.StreamStoppedEvent).OperationID, "do not mutate shared events")
	wire, err := json.Marshal(tagged)
	require.NoError(t, err)
	assert.NotContains(t, string(wire), "operation")
	assert.True(t, a.IsCurrentOperation(&contract.ElicitationClosedEvent{ElicitationID: "job"}), "detached closures outlive the foreground operation")
}

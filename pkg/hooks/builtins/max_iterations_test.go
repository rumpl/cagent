package builtins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/hooks/builtins"
)

// TestMaxIterationsTripsAfterLimit verifies the happy path: with a
// limit of 3, calls 1-3 are no-ops and call 4 returns a block decision.
// The reason carries the configured limit so the runtime's user-facing
// Error event explains why the run stopped.
func TestMaxIterationsTripsAfterLimit(t *testing.T) {
	t.Parallel()

	fn := lookup(t, builtins.MaxIterations)
	args := []string{"3"}

	for i := 1; i <= 3; i++ {
		out, err := fn(t.Context(), &hooks.Input{SessionID: "s1", ModelCallNumber: i}, args)
		require.NoErrorf(t, err, "call %d must not error", i)
		require.Nilf(t, out, "call %d (within limit) must not trip", i)
	}

	out, err := fn(t.Context(), &hooks.Input{SessionID: "s1", ModelCallNumber: 4}, args)
	require.NoError(t, err)
	require.NotNil(t, out, "fourth call (over limit) must trip")
	assert.Equal(t, hooks.DecisionBlockValue, out.Decision)
	assert.Contains(t, out.Reason, "3", "reason must include the configured limit")
}

// TestMaxIterationsIsStateless documents that the builtin relies only on
// ModelCallNumber supplied by the runtime, not on hidden per-session counters.
func TestMaxIterationsIsStateless(t *testing.T) {
	t.Parallel()

	fn := lookup(t, builtins.MaxIterations)
	args := []string{"2"}

	out, err := fn(t.Context(), &hooks.Input{SessionID: "A", ModelCallNumber: 3}, args)
	require.NoError(t, err)
	require.NotNil(t, out, "call number above the limit trips")

	out, err = fn(t.Context(), &hooks.Input{SessionID: "B", ModelCallNumber: 1}, args)
	require.NoError(t, err)
	require.Nil(t, out, "call number within the limit does not inherit state from another session")
}

// TestMaxIterationsNoOpWithoutValidLimit documents the lenient-args
// contract: a missing, non-integer, zero, or negative limit makes
// the builtin a no-op rather than tripping (the safer default for a
// misconfigured YAML).
func TestMaxIterationsNoOpWithoutValidLimit(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		nil,
		{},
		{"abc"},
		{"0"},
		{"-1"},
	}
	for _, args := range cases {
		fn := lookup(t, builtins.MaxIterations)
		out, err := fn(t.Context(), &hooks.Input{SessionID: "s", ModelCallNumber: 50}, args)
		require.NoError(t, err)
		require.Nilf(t, out, "args=%v: must never trip", args)
	}
}

// TestMaxIterationsIgnoresIncompleteInput pins the defensive guard:
// missing ModelCallNumber produces no output. This protects against
// future dispatch changes where an edge case might fire before_llm_call
// without the runtime call number populated.
func TestMaxIterationsIgnoresIncompleteInput(t *testing.T) {
	t.Parallel()

	fn := lookup(t, builtins.MaxIterations)

	out, err := fn(t.Context(), nil, []string{"1"})
	require.NoError(t, err)
	assert.Nil(t, out)

	out, err = fn(t.Context(), &hooks.Input{SessionID: "s"}, []string{"1"})
	require.NoError(t, err)
	assert.Nil(t, out)
}

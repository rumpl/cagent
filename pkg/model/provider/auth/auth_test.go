package auth

import (
	"context"
	"testing"

	"github.com/docker/cagent/pkg/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEnv map[string]string

func (e testEnv) Get(_ context.Context, name string) (string, bool) {
	v, ok := e[name]
	return v, ok
}

var _ environment.Provider = (testEnv)(nil)

func TestEnvTokenProvider(t *testing.T) {
	p := NewEnvTokenProvider("OPENAI_API_KEY")

	_, err := p.Token(t.Context(), testEnv{environment.DockerDesktopTokenEnv: ""})
	assert.Error(t, err)

	tok, err := p.Token(t.Context(), testEnv{"OPENAI_API_KEY": "abc"})
	require.NoError(t, err)
	assert.Equal(t, "abc", tok)
}

func TestDockerDesktopTokenProvider(t *testing.T) {
	p := NewDockerDesktopTokenProvider()

	_, err := p.Token(t.Context(), testEnv{environment.DockerDesktopTokenEnv: ""})
	assert.Error(t, err)

	tok, err := p.Token(t.Context(), testEnv{environment.DockerDesktopTokenEnv: "dd"})
	require.NoError(t, err)
	assert.Equal(t, "dd", tok)
}

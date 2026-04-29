package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/snapshot"
)

func requireGitForUndo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func TestUndoLastStep(t *testing.T) {
	requireGitForUndo(t)

	workTree := t.TempDir()
	mgr := snapshot.NewManager(t.TempDir())
	rt := &LocalRuntime{snapshotManager: mgr, workingDir: workTree}

	baseHash, err := mgr.Track(t.Context(), workTree)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v1\n"), 0o644))
	hash1, err := mgr.Track(t.Context(), workTree)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v2\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "b.txt"), []byte("b1\n"), 0o644))
	hash2, err := mgr.Track(t.Context(), workTree)
	require.NoError(t, err)

	sess := session.New(
		session.WithWorkingDir(workTree),
		session.WithStepSnapshots([]session.StepSnapshot{
			{
				AgentName:       "root",
				BeforeHash:      baseHash,
				AfterHash:       hash1,
				Files:           []string{"a.txt"},
				MessagePosition: 0,
			},
			{
				AgentName:       "root",
				BeforeHash:      hash1,
				AfterHash:       hash2,
				Files:           []string{"a.txt", "b.txt"},
				MessagePosition: 1,
			},
		}),
	)

	result, err := rt.UndoLastStep(t.Context(), sess)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.ElementsMatch(t, []string{"a.txt", "b.txt"}, result.Files)

	content, err := os.ReadFile(filepath.Join(workTree, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "v1\n", string(content))
	_, err = os.Stat(filepath.Join(workTree, "b.txt"))
	assert.True(t, os.IsNotExist(err))

	result, err = rt.UndoLastStep(t.Context(), sess)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"a.txt"}, result.Files)
	_, err = os.Stat(filepath.Join(workTree, "a.txt"))
	assert.True(t, os.IsNotExist(err))

	_, err = rt.UndoLastStep(t.Context(), sess)
	require.ErrorIs(t, err, ErrNothingToUndo)
}

func TestUndoLastStepOutOfSync(t *testing.T) {
	requireGitForUndo(t)

	workTree := t.TempDir()
	mgr := snapshot.NewManager(t.TempDir())
	rt := &LocalRuntime{snapshotManager: mgr, workingDir: workTree}

	baseHash, err := mgr.Track(t.Context(), workTree)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v1\n"), 0o644))
	hash1, err := mgr.Track(t.Context(), workTree)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("manual\n"), 0o644))

	sess := session.New(
		session.WithWorkingDir(workTree),
		session.WithStepSnapshots([]session.StepSnapshot{{
			AgentName:       "root",
			BeforeHash:      baseHash,
			AfterHash:       hash1,
			Files:           []string{"a.txt"},
			MessagePosition: 0,
		}}),
	)

	_, err = rt.UndoLastStep(t.Context(), sess)
	require.ErrorIs(t, err, ErrUndoOutOfSync)

	content, readErr := os.ReadFile(filepath.Join(workTree, "a.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "manual\n", string(content))
}

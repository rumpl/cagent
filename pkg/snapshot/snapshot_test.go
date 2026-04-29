package snapshot

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func TestTrackDiffAndRestore(t *testing.T) {
	requireGit(t)
	ctx := t.Context()
	workTree := t.TempDir()
	mgr := NewManager(t.TempDir())

	require.NoError(t, os.MkdirAll(filepath.Join(workTree, "dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, ".gitignore"), []byte("ignored.txt\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("one\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "dir", "b.txt"), []byte("two\n"), 0o644))

	repo, err := mgr.Repo(workTree)
	require.NoError(t, err)

	hash1, err := repo.Track(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, hash1)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("one updated\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(workTree, "dir", "b.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "c.txt"), []byte("three\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "ignored.txt"), []byte("should stay untouched\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "big.bin"), bytes.Repeat([]byte("x"), MaxTrackedFileSize+1), 0o644))

	hash2, err := repo.Track(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, hash2)
	assert.NotEqual(t, hash1, hash2)

	files, err := repo.ChangedFiles(ctx, hash1, hash2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", "c.txt", "dir/b.txt"}, files)

	diff, err := repo.Diff(ctx, hash1, hash2)
	require.NoError(t, err)
	assert.Contains(t, diff, "a.txt")
	assert.Contains(t, diff, "c.txt")
	assert.Contains(t, diff, "dir/b.txt")

	require.NoError(t, repo.Restore(ctx, hash1))

	content, err := os.ReadFile(filepath.Join(workTree, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(content))

	content, err = os.ReadFile(filepath.Join(workTree, "dir", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "two\n", string(content))

	_, err = os.Stat(filepath.Join(workTree, "c.txt"))
	assert.True(t, os.IsNotExist(err))

	content, err = os.ReadFile(filepath.Join(workTree, "ignored.txt"))
	require.NoError(t, err)
	assert.Equal(t, "should stay untouched\n", string(content))

	info, err := os.Stat(filepath.Join(workTree, "big.bin"))
	require.NoError(t, err)
	assert.Equal(t, int64(MaxTrackedFileSize+1), info.Size())
}

func TestRevert(t *testing.T) {
	requireGit(t)
	ctx := t.Context()
	workTree := t.TempDir()
	mgr := NewManager(t.TempDir())

	repo, err := mgr.Repo(workTree)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v1\n"), 0o644))
	hash1, err := repo.Track(ctx)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v2\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "b.txt"), []byte("b1\n"), 0o644))
	hash2, err := repo.Track(ctx)
	require.NoError(t, err)
	files12, err := repo.ChangedFiles(ctx, hash1, hash2)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workTree, "a.txt"), []byte("v3\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(workTree, "b.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(workTree, "c.txt"), []byte("c1\n"), 0o644))
	hash3, err := repo.Track(ctx)
	require.NoError(t, err)
	files23, err := repo.ChangedFiles(ctx, hash2, hash3)
	require.NoError(t, err)

	require.NoError(t, repo.Revert(ctx, []Patch{{BeforeHash: hash2, AfterHash: hash3, Files: files23}}))
	hashAfterFirstRevert, err := repo.Track(ctx)
	require.NoError(t, err)
	assert.Equal(t, hash2, hashAfterFirstRevert)

	content, err := os.ReadFile(filepath.Join(workTree, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "v2\n", string(content))
	content, err = os.ReadFile(filepath.Join(workTree, "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "b1\n", string(content))
	_, err = os.Stat(filepath.Join(workTree, "c.txt"))
	assert.True(t, os.IsNotExist(err))

	require.NoError(t, repo.Revert(ctx, []Patch{{BeforeHash: hash1, AfterHash: hash2, Files: files12}}))
	hashAfterSecondRevert, err := repo.Track(ctx)
	require.NoError(t, err)
	assert.Equal(t, hash1, hashAfterSecondRevert)

	content, err = os.ReadFile(filepath.Join(workTree, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "v1\n", string(content))
	_, err = os.Stat(filepath.Join(workTree, "b.txt"))
	assert.True(t, os.IsNotExist(err))
}

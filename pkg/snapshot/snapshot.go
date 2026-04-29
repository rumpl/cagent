package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/paths"
)

const (
	// MaxTrackedFileSize is the maximum file size included in snapshots.
	// Larger regular files are skipped to keep tree writes fast and lightweight.
	MaxTrackedFileSize = 2 * 1024 * 1024

	gitBatchSize  = 128
	gcInterval    = time.Hour
	gcPruneWindow = "7.days"
)

// Patch describes the file changes between two snapshot trees.
// BeforeHash is the tree state before a step and AfterHash is the tree state after it.
// Files contains the paths that changed during the step.
type Patch struct {
	BeforeHash string   `json:"before_hash"`
	AfterHash  string   `json:"after_hash"`
	Files      []string `json:"files,omitempty"`
}

// FileDiff is a structured diff for a single file between two snapshots.
type FileDiff struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Patch   string `json:"patch,omitempty"`
}

// Manager resolves per-worktree shadow repositories under a common base directory.
type Manager struct {
	baseDir string
}

// NewManager creates a new snapshot manager.
// If baseDir is empty, the default data directory is used.
func NewManager(baseDir string) *Manager {
	if baseDir == "" {
		baseDir = filepath.Join(paths.GetDataDir(), "snapshot")
	}
	return &Manager{baseDir: filepath.Clean(baseDir)}
}

// BaseDir returns the root directory used for shadow repositories.
func (m *Manager) BaseDir() string {
	return m.baseDir
}

// Repo returns the snapshot repo for a worktree.
func (m *Manager) Repo(workTree string) (*Repo, error) {
	if strings.TrimSpace(workTree) == "" {
		return nil, errors.New("snapshot worktree cannot be empty")
	}

	abs, err := filepath.Abs(workTree)
	if err != nil {
		return nil, fmt.Errorf("resolving snapshot worktree: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat snapshot worktree: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("snapshot worktree must be a directory: %s", abs)
	}

	repoRoot := filepath.Join(m.baseDir, repoDirName(abs))
	return &Repo{
		workTree: filepath.Clean(abs),
		rootDir:  repoRoot,
		gitDir:   filepath.Join(repoRoot, "git"),
		gcMarker: filepath.Join(repoRoot, "last-gc"),
	}, nil
}

// Track captures the current filesystem state of workTree and returns a git tree hash.
func (m *Manager) Track(ctx context.Context, workTree string) (string, error) {
	repo, err := m.Repo(workTree)
	if err != nil {
		return "", err
	}
	return repo.Track(ctx)
}

// ChangedFiles returns the files that changed between two snapshots for workTree.
func (m *Manager) ChangedFiles(ctx context.Context, workTree, fromHash, toHash string) ([]string, error) {
	repo, err := m.Repo(workTree)
	if err != nil {
		return nil, err
	}
	return repo.ChangedFiles(ctx, fromHash, toHash)
}

// Diff returns a unified diff between two snapshots for workTree.
func (m *Manager) Diff(ctx context.Context, workTree, fromHash, toHash string) (string, error) {
	repo, err := m.Repo(workTree)
	if err != nil {
		return "", err
	}
	return repo.Diff(ctx, fromHash, toHash)
}

// DiffFull returns structured per-file diffs between two snapshots for workTree.
func (m *Manager) DiffFull(ctx context.Context, workTree, fromHash, toHash string) ([]FileDiff, error) {
	repo, err := m.Repo(workTree)
	if err != nil {
		return nil, err
	}
	return repo.DiffFull(ctx, fromHash, toHash)
}

// Restore restores workTree to the exact state captured by hash.
func (m *Manager) Restore(ctx context.Context, workTree, hash string) error {
	repo, err := m.Repo(workTree)
	if err != nil {
		return err
	}
	return repo.Restore(ctx, hash)
}

// Revert surgically reverts the files described by patches in reverse order.
func (m *Manager) Revert(ctx context.Context, workTree string, patches []Patch) error {
	repo, err := m.Repo(workTree)
	if err != nil {
		return err
	}
	return repo.Revert(ctx, patches)
}

// Repo is a shadow git repository backed by the user's real worktree.
type Repo struct {
	workTree string
	rootDir  string
	gitDir   string
	gcMarker string
}

var repoLocks sync.Map // map[string]*sync.Mutex keyed by git dir

func lockForRepo(gitDir string) *sync.Mutex {
	lock, _ := repoLocks.LoadOrStore(gitDir, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// WorkTree returns the tracked worktree path.
func (r *Repo) WorkTree() string { return r.workTree }

// GitDir returns the shadow repository path.
func (r *Repo) GitDir() string { return r.gitDir }

// Track captures the current filesystem state and returns a git tree hash.
func (r *Repo) Track(ctx context.Context) (string, error) {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()
	return r.trackLocked(ctx)
}

// ChangedFiles returns the files changed between two snapshot trees.
func (r *Repo) ChangedFiles(ctx context.Context, fromHash, toHash string) ([]string, error) {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()
	return r.changedFilesLocked(ctx, fromHash, toHash)
}

// Diff returns a unified diff between two snapshot trees.
func (r *Repo) Diff(ctx context.Context, fromHash, toHash string) (string, error) {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()

	if err := r.ensureInitializedLocked(ctx); err != nil {
		return "", err
	}
	if strings.TrimSpace(fromHash) == "" || strings.TrimSpace(toHash) == "" {
		return "", errors.New("snapshot hashes cannot be empty")
	}

	out, err := r.gitOutputLocked(ctx, "diff", "--no-ext-diff", "--binary", "--unified=3", fromHash, toHash, "--")
	if err != nil {
		return "", fmt.Errorf("computing snapshot diff: %w", err)
	}
	return out, nil
}

// DiffFull returns structured per-file diffs between two snapshots.
func (r *Repo) DiffFull(ctx context.Context, fromHash, toHash string) ([]FileDiff, error) {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()

	files, err := r.diffPathsLocked(ctx, fromHash, toHash)
	if err != nil {
		return nil, err
	}

	diffs := make([]FileDiff, 0, len(files))
	for _, path := range files {
		patch, err := r.gitOutputLocked(ctx, "diff", "--no-ext-diff", "--binary", "--unified=3", fromHash, toHash, "--", path)
		if err != nil {
			return nil, fmt.Errorf("computing snapshot diff for %s: %w", path, err)
		}
		added, removed := countPatchLines(patch)
		diffs = append(diffs, FileDiff{
			Path:    path,
			Added:   added,
			Removed: removed,
			Patch:   patch,
		})
	}

	return diffs, nil
}

// Restore restores the worktree to the exact state captured by hash.
func (r *Repo) Restore(ctx context.Context, hash string) error {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()

	if strings.TrimSpace(hash) == "" {
		return errors.New("snapshot hash cannot be empty")
	}
	if err := r.ensureInitializedLocked(ctx); err != nil {
		return err
	}

	currentHash, err := r.trackLocked(ctx)
	if err != nil {
		return fmt.Errorf("capturing current snapshot before restore: %w", err)
	}

	currentFiles, err := r.listTreeFilesLocked(ctx, currentHash)
	if err != nil {
		return fmt.Errorf("listing current snapshot files: %w", err)
	}
	targetFiles, err := r.listTreeFilesLocked(ctx, hash)
	if err != nil {
		return fmt.Errorf("listing target snapshot files: %w", err)
	}

	if err := deletePaths(r.workTree, diffFileSets(currentFiles, targetFiles)); err != nil {
		return fmt.Errorf("deleting files during restore: %w", err)
	}

	if err := r.gitNoOutputLocked(ctx, "read-tree", "--empty"); err != nil {
		return fmt.Errorf("clearing snapshot index for restore: %w", err)
	}
	if err := r.gitNoOutputLocked(ctx, "read-tree", hash); err != nil {
		return fmt.Errorf("loading snapshot tree for restore: %w", err)
	}
	if err := r.gitNoOutputLocked(ctx, "checkout-index", "-a", "-f"); err != nil {
		return fmt.Errorf("checking out snapshot tree: %w", err)
	}

	return nil
}

// Revert surgically reverts files from patches in reverse order.
func (r *Repo) Revert(ctx context.Context, patches []Patch) error {
	lock := lockForRepo(r.gitDir)
	lock.Lock()
	defer lock.Unlock()

	if len(patches) == 0 {
		return nil
	}
	if err := r.ensureInitializedLocked(ctx); err != nil {
		return err
	}

	handled := make(map[string]struct{})
	treeFiles := make(map[string]map[string]struct{})

	for i := len(patches) - 1; i >= 0; i-- {
		patch := patches[i]
		if strings.TrimSpace(patch.BeforeHash) == "" {
			continue
		}

		filesForTree, ok := treeFiles[patch.BeforeHash]
		if !ok {
			var err error
			filesForTree, err = r.listTreeFilesLocked(ctx, patch.BeforeHash)
			if err != nil {
				return fmt.Errorf("listing snapshot files for revert: %w", err)
			}
			treeFiles[patch.BeforeHash] = filesForTree
		}

		var restoreFiles []string
		var deleteFiles []string
		for _, file := range patch.Files {
			if _, seen := handled[file]; seen || file == "" {
				continue
			}
			handled[file] = struct{}{}
			if _, ok := filesForTree[file]; ok {
				restoreFiles = append(restoreFiles, file)
			} else {
				deleteFiles = append(deleteFiles, file)
			}
		}

		if err := deletePaths(r.workTree, deleteFiles); err != nil {
			return fmt.Errorf("deleting files during revert: %w", err)
		}
		if err := r.restoreFilesFromTreeLocked(ctx, patch.BeforeHash, restoreFiles); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repo) trackLocked(ctx context.Context) (string, error) {
	if err := r.ensureInitializedLocked(ctx); err != nil {
		return "", err
	}
	if err := r.maybeRunGCLocked(ctx); err != nil {
		return "", err
	}

	if err := r.gitNoOutputLocked(ctx, "read-tree", "--empty"); err != nil {
		return "", fmt.Errorf("clearing snapshot index: %w", err)
	}

	files, err := r.listTrackableFilesLocked(ctx)
	if err != nil {
		return "", err
	}

	for start := 0; start < len(files); start += gitBatchSize {
		end := min(start+gitBatchSize, len(files))
		args := append([]string{"add", "--"}, files[start:end]...)
		if err := r.gitNoOutputLocked(ctx, args...); err != nil {
			return "", fmt.Errorf("staging snapshot files: %w", err)
		}
	}

	hash, err := r.gitOutputLocked(ctx, "write-tree")
	if err != nil {
		return "", fmt.Errorf("writing snapshot tree: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

func (r *Repo) changedFilesLocked(ctx context.Context, fromHash, toHash string) ([]string, error) {
	diffPaths, err := r.diffPathsLocked(ctx, fromHash, toHash)
	if err != nil {
		return nil, err
	}

	changed := make([]string, 0, len(diffPaths))
	for _, path := range diffPaths {
		keep, err := r.shouldReportPathLocked(ctx, path)
		if err != nil {
			return nil, err
		}
		if keep {
			changed = append(changed, path)
		}
	}

	sort.Strings(changed)
	return changed, nil
}

func (r *Repo) diffPathsLocked(ctx context.Context, fromHash, toHash string) ([]string, error) {
	if strings.TrimSpace(fromHash) == "" || strings.TrimSpace(toHash) == "" {
		return nil, errors.New("snapshot hashes cannot be empty")
	}
	if err := r.ensureInitializedLocked(ctx); err != nil {
		return nil, err
	}

	out, err := r.gitOutputLocked(ctx, "diff", "--name-only", "-z", "--no-ext-diff", "--no-renames", fromHash, toHash, "--")
	if err != nil {
		return nil, fmt.Errorf("listing changed snapshot files: %w", err)
	}

	var diffPaths []string
	for _, path := range parseNullSeparated(out) {
		if path != "" {
			diffPaths = append(diffPaths, path)
		}
	}
	return diffPaths, nil
}

func (r *Repo) ensureInitializedLocked(ctx context.Context) error {
	if err := os.MkdirAll(r.rootDir, 0o755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	if _, err := os.Stat(filepath.Join(r.gitDir, "HEAD")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat snapshot git dir: %w", err)
	}

	if err := r.gitNoOutputLocked(ctx, "init", "--quiet"); err != nil {
		return fmt.Errorf("initializing snapshot repository: %w", err)
	}
	return nil
}

func (r *Repo) maybeRunGCLocked(ctx context.Context) error {
	info, err := os.Stat(r.gcMarker)
	if err == nil && time.Since(info.ModTime()) < gcInterval {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat snapshot gc marker: %w", err)
	}

	if err := r.gitNoOutputLocked(ctx, "gc", "--quiet", "--prune="+gcPruneWindow); err != nil {
		return fmt.Errorf("running snapshot gc: %w", err)
	}
	if err := os.WriteFile(r.gcMarker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
		return fmt.Errorf("writing snapshot gc marker: %w", err)
	}
	return nil
}

func (r *Repo) listTrackableFilesLocked(ctx context.Context) ([]string, error) {
	out, err := r.gitOutputLocked(ctx, "ls-files", "-o", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("listing snapshot candidates: %w", err)
	}

	candidates := parseNullSeparated(out)
	files := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if path == "" {
			continue
		}
		absPath := filepath.Join(r.workTree, filepath.FromSlash(path))
		info, err := os.Lstat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat snapshot candidate %s: %w", path, err)
		}

		mode := info.Mode()
		switch {
		case mode.IsRegular():
			if info.Size() > MaxTrackedFileSize {
				continue
			}
		case mode&os.ModeSymlink != 0:
			// Git tracks the symlink itself, not its target, so size limits do not apply.
		default:
			continue
		}

		files = append(files, path)
	}

	sort.Strings(files)
	return files, nil
}

func (r *Repo) shouldReportPathLocked(ctx context.Context, path string) (bool, error) {
	absPath := filepath.Join(r.workTree, filepath.FromSlash(path))
	info, err := os.Lstat(absPath)
	switch {
	case err == nil:
		if info.Mode().IsRegular() && info.Size() > MaxTrackedFileSize {
			return false, nil
		}
		ignored, err := r.isIgnoredPathLocked(ctx, path)
		if err != nil {
			return false, err
		}
		return !ignored, nil
	case os.IsNotExist(err):
		return true, nil
	default:
		return false, fmt.Errorf("stat snapshot diff path %s: %w", path, err)
	}
}

func (r *Repo) isIgnoredPathLocked(ctx context.Context, path string) (bool, error) {
	cmd := r.gitCommand(ctx, "check-ignore", "--quiet", "--no-index", "--", path)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("checking ignore rules for %s: %w", path, err)
}

func (r *Repo) listTreeFilesLocked(ctx context.Context, hash string) (map[string]struct{}, error) {
	out, err := r.gitOutputLocked(ctx, "ls-tree", "-r", "-z", "--name-only", hash, "--")
	if err != nil {
		return nil, fmt.Errorf("listing snapshot tree %s: %w", hash, err)
	}

	files := make(map[string]struct{})
	for _, path := range parseNullSeparated(out) {
		if path == "" {
			continue
		}
		files[path] = struct{}{}
	}
	return files, nil
}

func (r *Repo) restoreFilesFromTreeLocked(ctx context.Context, hash string, files []string) error {
	if len(files) == 0 {
		return nil
	}

	sort.Strings(files)
	if err := r.gitNoOutputLocked(ctx, "read-tree", "--empty"); err != nil {
		return fmt.Errorf("clearing snapshot index for revert: %w", err)
	}
	if err := r.gitNoOutputLocked(ctx, "read-tree", hash); err != nil {
		return fmt.Errorf("loading snapshot tree for revert: %w", err)
	}

	for start := 0; start < len(files); start += gitBatchSize {
		end := min(start+gitBatchSize, len(files))
		args := append([]string{"checkout-index", "-f", "--"}, files[start:end]...)
		if err := r.gitNoOutputLocked(ctx, args...); err != nil {
			return fmt.Errorf("restoring files from snapshot: %w", err)
		}
	}

	return nil
}

func (r *Repo) gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.workTree
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+r.gitDir,
		"GIT_WORK_TREE="+r.workTree,
		"GIT_TERMINAL_PROMPT=0",
	)
	return cmd
}

func (r *Repo) gitNoOutputLocked(ctx context.Context, args ...string) error {
	_, err := r.gitOutputLocked(ctx, args...)
	return err
}

func (r *Repo) gitOutputLocked(ctx context.Context, args ...string) (string, error) {
	cmd := r.gitCommand(ctx, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, trimmed)
	}
	return string(out), nil
}

func repoDirName(workTree string) string {
	base := sanitizeSegment(filepath.Base(workTree))
	sum := sha256.Sum256([]byte(workTree))
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(sum[:8]))
}

func sanitizeSegment(value string) string {
	if value == "" || value == string(filepath.Separator) {
		return "workspace"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "workspace"
	}
	return result
}

func parseNullSeparated(value string) []string {
	parts := strings.Split(value, "\x00")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func diffFileSets(current, target map[string]struct{}) []string {
	diff := make([]string, 0)
	for path := range current {
		if _, ok := target[path]; !ok {
			diff = append(diff, path)
		}
	}
	return diff
}

func deletePaths(root string, fileList []string) error {
	if len(fileList) == 0 {
		return nil
	}

	sort.Slice(fileList, func(i, j int) bool {
		return len(fileList[i]) > len(fileList[j])
	})

	seen := make(map[string]struct{})
	for _, path := range fileList {
		absPath := filepath.Join(root, filepath.FromSlash(path))
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}

		if err := os.RemoveAll(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		removeEmptyParents(root, filepath.Dir(absPath))
	}

	return nil
}

func removeEmptyParents(root, dir string) {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	for dir != root && dir != "." && dir != string(filepath.Separator) {
		err := os.Remove(dir)
		if err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func countPatchLines(patch string) (added, removed int) {
	for line := range strings.SplitSeq(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

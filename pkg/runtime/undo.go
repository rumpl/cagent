package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/docker/docker-agent/pkg/session"
)

var (
	ErrUndoNotSupported = errors.New("snapshot undo is not supported by this runtime")
	ErrNothingToUndo    = errors.New("nothing to undo")
	ErrUndoOutOfSync    = errors.New("working tree no longer matches a recorded snapshot state")
	ErrUndoNoWorkingDir = errors.New("undo requires a session working directory")
)

// UndoResult describes a successfully reverted snapshot step.
type UndoResult struct {
	AgentName  string
	Files      []string
	BeforeHash string
	AfterHash  string
}

// SnapshotUndoer is implemented by runtimes that can undo the last
// file-changing snapshot step for a session.
type SnapshotUndoer interface {
	SupportsUndo() bool
	UndoLastStep(ctx context.Context, sess *session.Session) (*UndoResult, error)
}

// SupportsUndo reports whether this runtime can perform snapshot-based undo.
func (r *LocalRuntime) SupportsUndo() bool {
	return r.snapshotManager != nil
}

// UndoLastStep restores the worktree to the state before the most recent
// file-changing step whose after-snapshot matches the current worktree state.
//
// This makes repeated undo work naturally without storing extra cursor state:
// after one undo, the worktree matches the previous step's after-snapshot, so
// the next call undoes the previous step, and so on.
func (r *LocalRuntime) UndoLastStep(ctx context.Context, sess *session.Session) (*UndoResult, error) {
	if sess == nil {
		return nil, errors.New("session is nil")
	}
	if r.snapshotManager == nil {
		return nil, ErrUndoNotSupported
	}

	workTree := r.snapshotWorkTree(sess)
	if workTree == "" {
		return nil, ErrUndoNoWorkingDir
	}

	steps := sess.GetAllStepSnapshots()
	if len(steps) == 0 {
		return nil, ErrNothingToUndo
	}

	currentHash, err := r.snapshotManager.Track(ctx, workTree)
	if err != nil {
		return nil, fmt.Errorf("capturing current snapshot for undo: %w", err)
	}

	matchedBefore := false
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.BeforeHash == currentHash {
			matchedBefore = true
		}
		if step.BeforeHash == "" || step.AfterHash == "" || step.BeforeHash == step.AfterHash || len(step.Files) == 0 {
			continue
		}
		if step.AfterHash != currentHash {
			continue
		}

		if err := r.snapshotManager.Restore(ctx, workTree, step.BeforeHash); err != nil {
			return nil, fmt.Errorf("undoing snapshot step: %w", err)
		}

		return &UndoResult{
			AgentName:  step.AgentName,
			Files:      slices.Clone(step.Files),
			BeforeHash: step.BeforeHash,
			AfterHash:  step.AfterHash,
		}, nil
	}

	if matchedBefore {
		return nil, ErrNothingToUndo
	}
	return nil, ErrUndoOutOfSync
}

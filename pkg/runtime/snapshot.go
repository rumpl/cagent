package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/snapshot"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// BuiltinSnapshotTurnStart and BuiltinSnapshotTurnEnd are the registered
// names of the snapshot builtin pair. snapshot_turn_start fires on
// turn_start to capture the worktree's pre-turn shadow-git tree;
// snapshot_turn_end fires on turn_end to capture the post-turn tree,
// compute the diff, and append a [session.StepSnapshot] to the active
// session.
//
// The pair is auto-injected by [LocalRuntime.buildHooksExecutors] when
// the user has snapshots enabled in their global user config and the
// runtime has a [snapshot.Manager] configured. Users do not need to
// reference these names from YAML.
const (
	BuiltinSnapshotTurnStart = "snapshot_turn_start"
	BuiltinSnapshotTurnEnd   = "snapshot_turn_end"
)

// snapshotState tracks the per-session pre-turn hash recorded by
// [LocalRuntime.snapshotTurnStartBuiltin] and read by
// [LocalRuntime.snapshotTurnEndBuiltin]. Concurrent runs of different
// sessions don't overlap because every entry is keyed by SessionID,
// and a single session is processed by exactly one [RunStream]
// goroutine at a time.
type snapshotState struct {
	mu     sync.Mutex
	before map[string]string // SessionID -> pre-turn tree hash
}

func newSnapshotState() *snapshotState {
	return &snapshotState{before: map[string]string{}}
}

func (s *snapshotState) put(sessionID, hash string) {
	s.mu.Lock()
	s.before[sessionID] = hash
	s.mu.Unlock()
}

func (s *snapshotState) take(sessionID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, ok := s.before[sessionID]
	if ok {
		delete(s.before, sessionID)
	}
	return hash, ok
}

func (s *snapshotState) clearSession(sessionID string) {
	s.mu.Lock()
	delete(s.before, sessionID)
	s.mu.Unlock()
}

// snapshotsEnabled reports whether the snapshot builtin pair should be
// auto-injected for agents on this runtime. False when no
// [snapshot.Manager] has been wired (via [WithSnapshotManager]) or when
// the user has explicitly opted out via the global user config.
func (r *LocalRuntime) snapshotsEnabled() bool {
	if r.snapshotManager == nil {
		return false
	}
	settings := userconfig.Get()
	return settings.GetSnapshots()
}

// applySnapshotDefault appends the snapshot turn_start / turn_end
// builtin hooks to cfg when snapshots are enabled at runtime level.
// Mirrors the role of [builtins.ApplyAgentDefaults] for runtime-private
// builtins. The helper accepts (and may return) a nil cfg so callers
// can chain it without an extra branch.
func (r *LocalRuntime) applySnapshotDefault(cfg *hooks.Config, _ *agent.Agent) *hooks.Config {
	if !r.snapshotsEnabled() {
		return cfg
	}
	if cfg == nil {
		cfg = &hooks.Config{}
	}
	cfg.TurnStart = append(cfg.TurnStart, hooks.Hook{
		Type:    hooks.HookTypeBuiltin,
		Command: BuiltinSnapshotTurnStart,
	})
	cfg.TurnEnd = append(cfg.TurnEnd, hooks.Hook{
		Type:    hooks.HookTypeBuiltin,
		Command: BuiltinSnapshotTurnEnd,
	})
	return cfg
}

// snapshotTurnStartBuiltin captures the worktree's pre-turn tree hash
// and stashes it on the runtime's per-session state. Failures are
// logged and swallowed: snapshot tracking is best-effort and must not
// break the agent loop.
func (r *LocalRuntime) snapshotTurnStartBuiltin(ctx context.Context, in *hooks.Input, _ []string) (*hooks.Output, error) {
	if r.snapshotManager == nil || in == nil || in.SessionID == "" {
		return nil, nil
	}
	sess := r.lookupActiveSession(in.SessionID)
	if sess == nil {
		return nil, nil
	}
	workTree := r.snapshotWorkTree(sess)
	if workTree == "" {
		return nil, nil
	}

	repo, err := r.snapshotManager.Repo(workTree)
	if err != nil {
		slog.Debug("snapshot_turn_start: repo unavailable", "session_id", in.SessionID, "error", err)
		return nil, nil
	}

	hash, err := repo.Track(ctx)
	if err != nil {
		slog.Debug("snapshot_turn_start: track failed", "session_id", in.SessionID, "error", err)
		return nil, nil
	}
	r.snapshotBuiltinState().put(in.SessionID, hash)
	return nil, nil
}

// snapshotTurnEndBuiltin captures the worktree's post-turn tree hash,
// computes the diff against the pre-turn hash recorded by
// [LocalRuntime.snapshotTurnStartBuiltin], and appends a
// [session.StepSnapshot] to the active session. A no-op when no
// pre-turn hash was recorded (turn_end fired without a matching
// turn_start, or the start builtin failed).
func (r *LocalRuntime) snapshotTurnEndBuiltin(ctx context.Context, in *hooks.Input, _ []string) (*hooks.Output, error) {
	if r.snapshotManager == nil || in == nil || in.SessionID == "" {
		return nil, nil
	}
	sess := r.lookupActiveSession(in.SessionID)
	if sess == nil {
		return nil, nil
	}
	workTree := r.snapshotWorkTree(sess)
	if workTree == "" {
		return nil, nil
	}

	beforeHash, ok := r.snapshotBuiltinState().take(in.SessionID)
	if !ok {
		return nil, nil
	}

	repo, err := r.snapshotManager.Repo(workTree)
	if err != nil {
		slog.Debug("snapshot_turn_end: repo unavailable", "session_id", in.SessionID, "error", err)
		return nil, nil
	}

	afterHash, err := repo.Track(ctx)
	if err != nil {
		slog.Debug("snapshot_turn_end: track failed", "session_id", in.SessionID, "error", err)
		return nil, nil
	}

	files, err := repo.ChangedFiles(ctx, beforeHash, afterHash)
	if err != nil {
		slog.Debug("snapshot_turn_end: diff failed",
			"session_id", in.SessionID, "before", beforeHash, "after", afterHash, "error", err)
		return nil, nil
	}

	step := session.StepSnapshot{
		AgentName:       in.AgentName,
		BeforeHash:      beforeHash,
		AfterHash:       afterHash,
		Files:           files,
		MessagePosition: len(sess.GetAllMessages()),
		CreatedAt:       r.now().UTC().Format(time.RFC3339),
	}
	sess.AddStepSnapshot(step)

	if err := r.persistStepSnapshot(ctx, sess); err != nil {
		slog.Warn("snapshot_turn_end: persist failed",
			"session_id", in.SessionID, "error", err)
	}
	return nil, nil
}

// snapshotBuiltinState lazily allocates the runtime's
// [snapshotState]. The state lives on the runtime (not on a
// per-session struct) because builtins are registered once during
// [NewLocalRuntime] and need a stable handle to look up per-session
// pre-turn hashes from any goroutine.
func (r *LocalRuntime) snapshotBuiltinState() *snapshotState {
	r.snapshotStateOnce.Do(func() {
		r.snapshotStateRef = newSnapshotState()
	})
	return r.snapshotStateRef
}

// persistStepSnapshot saves the session's updated snapshot list to the
// configured store. Best-effort: a store error is logged but does not
// abort the agent loop. Sessions held only in [InMemorySessionStore]
// pick up the new step on the next UpdateSession call.
func (r *LocalRuntime) persistStepSnapshot(ctx context.Context, sess *session.Session) error {
	if r.sessionStore == nil {
		return nil
	}
	return r.sessionStore.UpdateSession(ctx, sess)
}

// trackActiveSession registers sess so that snapshot builtins can
// resolve its [*session.Session] from a [hooks.Input.SessionID]. The
// returned cleanup is paired with a defer at the top of [RunStream].
func (r *LocalRuntime) trackActiveSession(sess *session.Session) func() {
	if sess == nil || sess.ID == "" {
		return func() {}
	}
	r.activeSessions.Store(sess.ID, sess)
	return func() {
		r.activeSessions.Delete(sess.ID)
		if r.snapshotStateRef != nil {
			r.snapshotStateRef.clearSession(sess.ID)
		}
	}
}

// lookupActiveSession resolves a session ID to its live
// [*session.Session]. Returns nil if the session is not currently being
// run by this runtime.
func (r *LocalRuntime) lookupActiveSession(sessionID string) *session.Session {
	if v, ok := r.activeSessions.Load(sessionID); ok {
		if sess, ok := v.(*session.Session); ok {
			return sess
		}
	}
	return nil
}

// WithSnapshotManager wires a shared [snapshot.Manager] into the
// runtime so the snapshot builtin pair can be auto-injected. Pass
// [snapshot.NewManager] with the user-data directory you want the
// shadow-git repos stored under.
func WithSnapshotManager(m *snapshot.Manager) Opt {
	return func(r *LocalRuntime) {
		r.snapshotManager = m
	}
}

// snapshotWorkTree resolves the worktree path the snapshot manager
// should track for sess. Falls back to the runtime's WorkingDir when
// the session has none configured (e.g. evaluation harness).
func (r *LocalRuntime) snapshotWorkTree(sess *session.Session) string {
	if sess != nil && strings.TrimSpace(sess.WorkingDir) != "" {
		return sess.WorkingDir
	}
	return strings.TrimSpace(r.workingDir)
}

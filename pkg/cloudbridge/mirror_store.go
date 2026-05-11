package cloudbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/cloudauth"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// mirrorQueueSize is the buffered channel size for background mirror jobs.
// Sized generously to absorb per-token bursts during streaming responses
// without dropping events.
const mirrorQueueSize = 4096

// Enabled reports whether the cloud bridge should be active for this process.
//
// True iff:
//  1. ~/.config/cagent/credentials.json exists (user signed in), and
//  2. cloud.enabled is true in user config — or the cloud block is absent
//     entirely (signing in implies opt-in).
//
// Failures to read user config are non-fatal — we fall back to (1) alone.
func Enabled() bool {
	if _, err := os.Stat(cloudauth.CredentialsPath()); err != nil {
		return false
	}
	cfg, err := userconfig.Load()
	if err != nil {
		return true
	}
	if cfg.Cloud == nil {
		return true
	}
	return cfg.Cloud.Enabled
}

// endpoint returns the configured AP endpoint (cloud.endpoint, else the value
// stored in credentials.json, else the default).
func endpoint() string {
	cfg, err := userconfig.Load()
	if err == nil && cfg.Cloud != nil && cfg.Cloud.Endpoint != "" {
		return cfg.Cloud.Endpoint
	}
	if creds, err := cloudauth.LoadCredentials(); err == nil && creds.APEndpoint != "" {
		return creds.APEndpoint
	}
	return cloudauth.DefaultEndpoint
}

// hostLabel is os.Hostname() cached at first use.
var (
	hostnameOnce  sync.Once
	hostnameValue string
)

func hostLabel() string {
	hostnameOnce.Do(func() {
		if h, err := os.Hostname(); err == nil {
			hostnameValue = h
		}
	})
	return hostnameValue
}

// MirrorStore decorates a session.Store and asynchronously mirrors mutating
// operations to the Agentic Platform's LocalAgentService.
//
// All AP traffic is best-effort: failures are retried with exponential
// backoff and eventually dropped on the floor. The inner store remains the
// source of truth.
type MirrorStore struct {
	inner session.Store

	q       chan mirrorJob
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	stopped sync.Once

	// optedIn tracks external_ids the user has explicitly opted in to remote
	// control via the /remote-control slash command. Mirror operations are a
	// no-op for any session not in this set — even when signed in, sessions
	// stay private to the local host until the user opts them in.
	optedIn sync.Map // map[string]struct{}

	// registered tracks external_ids successfully registered with AP during
	// this process. It lets the worker pre-promote message/update jobs into
	// a Register for sessions AP hasn't seen yet.
	registered sync.Map // map[string]struct{}

	// apMessageIDs maps local (inner-store) message IDs to AP message IDs.
	// Populated by successful AddLocalMessage responses; consulted by
	// UpdateLocalMessage to address the correct row server-side.
	apMessageIDs sync.Map // map[int64]int64

	// msgSession maps local (inner-store) message IDs to the external session
	// ID that owns them. Populated by AddMessage so UpdateMessage doesn't have
	// to rediscover the owning session (the SQLite-backed inner store loads
	// items without their row IDs, which made any post-hoc lookup impossible).
	msgSession sync.Map // map[int64]string
}

// Wrap returns a MirrorStore wrapping inner. The background worker is started
// immediately; callers should call Close on the wrapper (via the Store
// interface) to flush pending jobs at shutdown.
//
// The concrete *MirrorStore type is returned so callers can hand it to
// [NewEventObserver]; assigning to a [session.Store]-typed variable is also
// safe and is the common usage.
func Wrap(inner session.Store) *MirrorStore {
	ctx, cancel := context.WithCancel(context.Background())
	m := &MirrorStore{
		inner:  inner,
		q:      make(chan mirrorJob, mirrorQueueSize),
		cancel: cancel,
	}
	m.wg.Go(func() { m.worker(ctx) })
	return m
}

// IsActive reports whether the given session is currently mirrored to AP.
// Sessions become active via [ActivateSession] (typically the /remote-control
// slash command) and stop being active via [DeactivateSession].
func (m *MirrorStore) IsActive(externalID string) bool {
	_, ok := m.optedIn.Load(externalID)
	return ok
}

// ActivateSession opts the given session in for remote control. It marks
// the session active and enqueues a RegisterLocalSession so AP learns about
// it. Only events that happen *after* activation are mirrored — historical
// messages stay local. This keeps activation O(1) regardless of session
// length; if you want to view the full transcript remotely, view it locally.
//
// Safe to call repeatedly: RegisterLocalSession is an upsert and the active
// flag is idempotent.
func (m *MirrorStore) ActivateSession(ctx context.Context, externalID string) error {
	if externalID == "" {
		return errors.New("cloudbridge: external_id is required")
	}
	if _, loaded := m.optedIn.LoadOrStore(externalID, struct{}{}); loaded {
		return nil // already active
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := m.inner.GetSession(lookupCtx, externalID)
	if err != nil {
		m.optedIn.Delete(externalID)
		return fmt.Errorf("cloudbridge: load session %s: %w", externalID, err)
	}
	itemsJSON, mErr := json.Marshal(sess.Messages)
	if mErr != nil {
		slog.Warn("cloudbridge: failed to marshal session items for activation",
			"external_id", externalID, "error", mErr)
		itemsJSON = nil
	}
	m.enqueue(mirrorJob{
		kind:       jobRegister,
		externalID: sess.ID,
		body: registerSessionBody{
			ExternalID: sess.ID,
			Title:      sess.Title,
			AgentName:  "docker-agent",
			HostLabel:  hostLabel(),
			CreatedAt:  sess.CreatedAt.UTC().Format(time.RFC3339),
			ItemsJSON:  itemsJSON,
		},
	})
	return nil
}

// DeactivateSession opts the session out. Subsequent mirror operations are
// silently dropped. AP retains whatever was already mirrored — there is no
// remote teardown call.
func (m *MirrorStore) DeactivateSession(externalID string) {
	m.optedIn.Delete(externalID)
}

// === job types ===

type jobKind int

const (
	jobRegister jobKind = iota
	jobUpdateSession
	jobAddMessage
	jobUpdateMessage
	jobAddSummary
	jobUpdateTokens
	jobPublishEvent
)

// mirrorJob carries one queued AP call. externalID identifies the session and
// is used both for the pre-promote Register-on-unknown check and for logging.
type mirrorJob struct {
	kind       jobKind
	externalID string
	// localMsgID is the inner-store message ID for jobAddMessage and
	// jobUpdateMessage. AP IDs are resolved via apMessageIDs at dispatch time.
	localMsgID int64
	// body is the JSON payload for kinds that send static request bodies
	// (jobRegister, jobUpdateSession, jobAddSummary, jobUpdateTokens).
	body any
	// messageJSON is the native cagent JSON for jobAddMessage / jobUpdateMessage.
	messageJSON []byte
	agentName   string
	implicit    bool
	// eventType / eventJSON populate jobPublishEvent.
	eventType string
	eventJSON []byte
}

// === session.Store delegation ===

func (m *MirrorStore) AddSession(ctx context.Context, s *session.Session) error {
	if err := m.inner.AddSession(ctx, s); err != nil {
		return err
	}
	if m.IsActive(s.ID) {
		m.enqueueRegister(s)
	}
	return nil
}

func (m *MirrorStore) UpdateSession(ctx context.Context, s *session.Session) error {
	if err := m.inner.UpdateSession(ctx, s); err != nil {
		return err
	}
	if !m.IsActive(s.ID) {
		return nil
	}
	m.enqueue(mirrorJob{
		kind:       jobUpdateSession,
		externalID: s.ID,
		body: updateSessionBody{
			ExternalID: s.ID,
			Title:      s.Title,
		},
	})
	return nil
}

func (m *MirrorStore) GetSession(ctx context.Context, id string) (*session.Session, error) {
	return m.inner.GetSession(ctx, id)
}

func (m *MirrorStore) GetSessions(ctx context.Context) ([]*session.Session, error) {
	return m.inner.GetSessions(ctx)
}

func (m *MirrorStore) GetSessionSummaries(ctx context.Context) ([]session.Summary, error) {
	return m.inner.GetSessionSummaries(ctx)
}

func (m *MirrorStore) DeleteSession(ctx context.Context, id string) error {
	return m.inner.DeleteSession(ctx, id)
}

func (m *MirrorStore) SetSessionStarred(ctx context.Context, id string, starred bool) error {
	return m.inner.SetSessionStarred(ctx, id, starred)
}

func (m *MirrorStore) AddMessage(ctx context.Context, sessionID string, msg *session.Message) (int64, error) {
	localID, err := m.inner.AddMessage(ctx, sessionID, msg)
	if err != nil {
		return localID, err
	}
	if !m.IsActive(sessionID) {
		return localID, nil
	}
	m.msgSession.Store(localID, sessionID)
	raw, mErr := marshalChatMessage(msg)
	if mErr != nil {
		slog.Warn("cloudbridge: skipping AddMessage mirror — message marshal failed",
			"external_id", sessionID, "local_message_id", localID, "error", mErr)
		return localID, nil
	}
	m.enqueue(mirrorJob{
		kind:        jobAddMessage,
		externalID:  sessionID,
		localMsgID:  localID,
		agentName:   msg.AgentName,
		implicit:    msg.Implicit,
		messageJSON: raw,
	})
	return localID, nil
}

func (m *MirrorStore) UpdateMessage(ctx context.Context, messageID int64, msg *session.Message) error {
	if err := m.inner.UpdateMessage(ctx, messageID, msg); err != nil {
		return err
	}
	var externalID string
	if v, ok := m.msgSession.Load(messageID); ok {
		externalID, _ = v.(string)
	}
	// Only mirror updates for sessions whose Add was mirrored too. If the
	// Add happened pre-activation, msgSession is unset and we skip silently.
	if externalID == "" || !m.IsActive(externalID) {
		return nil
	}
	raw, mErr := marshalChatMessage(msg)
	if mErr != nil {
		slog.Warn("cloudbridge: skipping UpdateMessage mirror — message marshal failed",
			"local_message_id", messageID, "error", mErr)
		return nil
	}
	m.enqueue(mirrorJob{
		kind:        jobUpdateMessage,
		externalID:  externalID,
		localMsgID:  messageID,
		messageJSON: raw,
	})
	return nil
}

func (m *MirrorStore) AddSubSession(ctx context.Context, parentSessionID string, sub *session.Session) error {
	return m.inner.AddSubSession(ctx, parentSessionID, sub)
}

func (m *MirrorStore) AddSummary(ctx context.Context, sessionID, summary string, firstKeptEntry int) error {
	if err := m.inner.AddSummary(ctx, sessionID, summary, firstKeptEntry); err != nil {
		return err
	}
	if !m.IsActive(sessionID) {
		return nil
	}
	m.enqueue(mirrorJob{
		kind:       jobAddSummary,
		externalID: sessionID,
		body: addSummaryBody{
			ExternalID:     sessionID,
			Summary:        summary,
			FirstKeptEntry: int32(firstKeptEntry),
		},
	})
	return nil
}

func (m *MirrorStore) UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, cost float64) error {
	if err := m.inner.UpdateSessionTokens(ctx, sessionID, inputTokens, outputTokens, cost); err != nil {
		return err
	}
	if !m.IsActive(sessionID) {
		return nil
	}
	m.enqueue(mirrorJob{
		kind:       jobUpdateTokens,
		externalID: sessionID,
		body: updateTokensBody{
			ExternalID:   sessionID,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Cost:         cost,
		},
	})
	return nil
}

func (m *MirrorStore) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	if err := m.inner.UpdateSessionTitle(ctx, sessionID, title); err != nil {
		return err
	}
	if !m.IsActive(sessionID) {
		return nil
	}
	m.enqueue(mirrorJob{
		kind:       jobUpdateSession,
		externalID: sessionID,
		body: updateSessionBody{
			ExternalID: sessionID,
			Title:      title,
		},
	})
	return nil
}

func (m *MirrorStore) Close() error {
	m.stopped.Do(func() {
		// Stop accepting new jobs and let the worker drain.
		close(m.q)
		m.wg.Wait()
		m.cancel()
	})
	return m.inner.Close()
}

// === request/response body types ===
//
// All field names are camelCase to match Connect-RPC's proto3 canonical JSON.
// int64 fields are tagged ",string" because proto3 JSON encodes int64 as a
// JSON string for safe round-tripping with non-Go consumers.

type registerSessionBody struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title,omitempty"`
	AgentName  string `json:"agentName,omitempty"`
	HostLabel  string `json:"hostLabel,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	// ItemsJSON, when non-empty, is the full session transcript at activation
	// time. Marshaled by encoding/json as base64 (proto3 `bytes` shape) and
	// bulk-inserted server-side iff the AP session has no items yet.
	ItemsJSON []byte `json:"itemsJson,omitempty"`
}

type registerSessionResp struct {
	ID string `json:"id"`
}

type updateSessionBody struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title,omitempty"`
}

type addLocalMessageBody struct {
	ExternalID  string `json:"externalId"`
	AgentName   string `json:"agentName,omitempty"`
	Implicit    bool   `json:"implicit,omitempty"`
	MessageJSON []byte `json:"messageJson"`
}

type addLocalMessageResp struct {
	// MessageID is decoded with a custom unmarshaller because proto3 JSON
	// encodes int64 as a quoted string by default, but some servers / proxies
	// emit it as a number. Accept both.
	MessageID     flexInt64 `json:"messageId"`
	EventSequence string    `json:"eventSequence,omitempty"`
}

// flexInt64 unmarshals from either a JSON number or a JSON string holding an
// integer. Marshals as a JSON string (proto3-canonical).
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*f = flexInt64(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*f = flexInt64(n)
	return nil
}

type updateLocalMessageBody struct {
	ExternalID  string `json:"externalId"`
	MessageID   int64  `json:"messageId,string"`
	MessageJSON []byte `json:"messageJson"`
}

type addSummaryBody struct {
	ExternalID     string `json:"externalId"`
	Summary        string `json:"summary"`
	FirstKeptEntry int32  `json:"firstKeptEntry,omitempty"`
}

type updateTokensBody struct {
	ExternalID   string  `json:"externalId"`
	InputTokens  int64   `json:"inputTokens,string"`
	OutputTokens int64   `json:"outputTokens,string"`
	Cost         float64 `json:"cost"`
}

// === worker ===

func (m *MirrorStore) enqueueRegister(s *session.Session) {
	m.enqueue(mirrorJob{
		kind:       jobRegister,
		externalID: s.ID,
		body: registerSessionBody{
			ExternalID: s.ID,
			Title:      s.Title,
			AgentName:  "docker-agent",
			HostLabel:  hostLabel(),
			CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
		},
	})
}

func (m *MirrorStore) enqueue(job mirrorJob) {
	select {
	case m.q <- job:
	default:
		slog.Warn("cloudbridge: mirror queue full, dropping job",
			"kind", job.kind, "external_id", job.externalID)
	}
}

func (m *MirrorStore) worker(ctx context.Context) {
	for job := range m.q {
		m.runJobWithRetry(ctx, job)
	}
}

func (m *MirrorStore) runJobWithRetry(ctx context.Context, job mirrorJob) {
	const maxAttempts = 5
	backoff := time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := m.runJob(ctx, job)
		if err == nil {
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Debug("cloudbridge: mirror job failed",
			"kind", job.kind, "external_id", job.externalID,
			"attempt", attempt, "error", err)
		if attempt == maxAttempts {
			slog.Warn("cloudbridge: mirror job giving up",
				"kind", job.kind, "external_id", job.externalID, "error", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (m *MirrorStore) runJob(ctx context.Context, job mirrorJob) error {
	// Ensure the owning session is registered before sending any mirror call
	// that addresses it by external_id. Avoids guaranteed not_found rejections
	// on the first message after backfill or for sessions created pre-sign-in.
	if job.externalID != "" {
		if _, ok := m.registered.Load(job.externalID); !ok && job.kind != jobRegister {
			if err := m.registerFromInner(ctx, job.externalID); err != nil {
				return err
			}
		}
	}

	switch job.kind {
	case jobRegister:
		return m.callRegister(ctx, job)
	case jobUpdateSession:
		return m.callUnary(ctx, job, "/api/platform.v1.LocalAgentService/UpdateLocalSession")
	case jobAddMessage:
		return m.callAddMessage(ctx, job)
	case jobUpdateMessage:
		return m.callUpdateMessage(ctx, job)
	case jobAddSummary:
		return m.callUnary(ctx, job, "/api/platform.v1.LocalAgentService/AddLocalSummary")
	case jobUpdateTokens:
		return m.callUnary(ctx, job, "/api/platform.v1.LocalAgentService/UpdateLocalSessionTokens")
	case jobPublishEvent:
		return m.callPublishEvent(ctx, job)
	}
	return nil
}

// PublishEvent enqueues a runtime event for delivery to the AP event bus.
// eventJSON should be json.Marshal of a runtime.Event. The call is
// non-blocking; events are sent best-effort by the background worker.
func (m *MirrorStore) PublishEvent(externalID, eventType string, eventJSON []byte) {
	if externalID == "" || len(eventJSON) == 0 {
		return
	}
	m.enqueue(mirrorJob{
		kind:       jobPublishEvent,
		externalID: externalID,
		eventType:  eventType,
		eventJSON:  eventJSON,
	})
}

type publishEventBody struct {
	ExternalID string `json:"externalId"`
	EventType  string `json:"eventType,omitempty"`
	EventJSON  []byte `json:"eventJson"`
}

func (m *MirrorStore) callPublishEvent(ctx context.Context, job mirrorJob) error {
	body := publishEventBody{
		ExternalID: job.externalID,
		EventType:  job.eventType,
		EventJSON:  job.eventJSON,
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := CallAP(callCtx, endpoint(),
		"/api/platform.v1.LocalAgentService/PublishLocalEvent", body, nil)
	if err != nil && IsCode(err, "not_found") {
		m.registered.Delete(job.externalID)
		return m.registerFromInner(ctx, job.externalID)
	}
	return err
}

func (m *MirrorStore) callRegister(ctx context.Context, job mirrorJob) error {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var resp registerSessionResp
	if err := CallAP(callCtx, endpoint(),
		"/api/platform.v1.LocalAgentService/RegisterLocalSession", job.body, &resp); err != nil {
		return err
	}
	m.registered.Store(job.externalID, struct{}{})
	return nil
}

func (m *MirrorStore) callUnary(ctx context.Context, job mirrorJob, method string) error {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := CallAP(callCtx, endpoint(), method, job.body, nil)
	if err != nil && IsCode(err, "not_found") && job.externalID != "" {
		m.registered.Delete(job.externalID)
		return m.registerFromInner(ctx, job.externalID)
	}
	return err
}

func (m *MirrorStore) callAddMessage(ctx context.Context, job mirrorJob) error {
	body := addLocalMessageBody{
		ExternalID:  job.externalID,
		AgentName:   job.agentName,
		Implicit:    job.implicit,
		MessageJSON: job.messageJSON,
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var resp addLocalMessageResp
	if err := CallAP(callCtx, endpoint(),
		"/api/platform.v1.LocalAgentService/AddLocalMessage", body, &resp); err != nil {
		return err
	}
	if job.localMsgID != 0 && resp.MessageID != 0 {
		m.apMessageIDs.Store(job.localMsgID, int64(resp.MessageID))
		slog.Debug("cloudbridge: AddLocalMessage ok",
			"external_id", job.externalID,
			"local_message_id", job.localMsgID,
			"ap_message_id", int64(resp.MessageID))
	} else {
		slog.Warn("cloudbridge: AddLocalMessage returned no messageId",
			"external_id", job.externalID,
			"local_message_id", job.localMsgID,
			"resp", resp)
	}
	return nil
}

func (m *MirrorStore) callUpdateMessage(ctx context.Context, job mirrorJob) error {
	apIDAny, ok := m.apMessageIDs.Load(job.localMsgID)
	if !ok {
		// The corresponding AddLocalMessage hasn't completed (or failed).
		// Drop silently — a subsequent Update job carries the same final
		// state, and if Add never succeeds there's nothing to update.
		slog.Debug("cloudbridge: skipping UpdateMessage mirror — no AP message id",
			"local_message_id", job.localMsgID)
		return nil
	}
	apID := apIDAny.(int64)
	externalID := job.externalID
	if externalID == "" {
		if v, ok := m.msgSession.Load(job.localMsgID); ok {
			externalID, _ = v.(string)
		}
	}
	if externalID == "" {
		slog.Debug("cloudbridge: skipping UpdateMessage mirror — no owning session",
			"local_message_id", job.localMsgID)
		return nil
	}
	if _, registered := m.registered.Load(externalID); !registered {
		if err := m.registerFromInner(ctx, externalID); err != nil {
			return err
		}
	}
	body := updateLocalMessageBody{
		ExternalID:  externalID,
		MessageID:   apID,
		MessageJSON: job.messageJSON,
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	slog.Debug("cloudbridge: UpdateLocalMessage",
		"external_id", externalID,
		"local_message_id", job.localMsgID,
		"ap_message_id", apID,
		"bytes", len(job.messageJSON))
	return CallAP(callCtx, endpoint(),
		"/api/platform.v1.LocalAgentService/UpdateLocalMessage", body, nil)
}

// registerFromInner reloads a session from the inner store and sends a
// RegisterLocalSession. Used to ensure the AP knows about a session before
// we mirror messages/updates against it.
func (m *MirrorStore) registerFromInner(ctx context.Context, externalID string) error {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := m.inner.GetSession(lookupCtx, externalID)
	if err != nil {
		return fmt.Errorf("reload session %s for register: %w", externalID, err)
	}
	body := registerSessionBody{
		ExternalID: s.ID,
		Title:      s.Title,
		AgentName:  "docker-agent",
		HostLabel:  hostLabel(),
		CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
	}
	callCtx, callCancel := context.WithTimeout(ctx, 15*time.Second)
	defer callCancel()
	slog.Debug("cloudbridge: registering session before mirror call",
		"external_id", externalID)
	var resp registerSessionResp
	if err := CallAP(callCtx, endpoint(),
		"/api/platform.v1.LocalAgentService/RegisterLocalSession", body, &resp); err != nil {
		return err
	}
	m.registered.Store(externalID, struct{}{})
	return nil
}

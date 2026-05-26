package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// mockRunner implements Runner for testing. It mirrors the runtime's
// new contract: RunAgent returns immediately after setup; an internal
// goroutine drives runs in response to params.ResumeSignal and pushes
// results on params.Completed.
type mockRunner struct {
	subAgentNames []string
	runResult     *RunResult
	runDelay      time.Duration // optional delay to simulate work
	setupErr      string        // when non-empty, RunAgent reports a setup failure
	lastCtxDone   <-chan struct{}
	runs          atomic.Int32 // number of times the driver invoked a run
}

func (m *mockRunner) CurrentAgentSubAgentNames() []string { return m.subAgentNames }
func (m *mockRunner) RunAgent(ctx context.Context, params RunParams) *RunResult {
	if m.setupErr != "" {
		return &RunResult{ErrMsg: m.setupErr}
	}
	m.lastCtxDone = ctx.Done()
	go func() {
		for {
			select {
			case _, ok := <-params.ResumeSignal:
				if !ok {
					return
				}
				if m.runDelay > 0 {
					select {
					case <-time.After(m.runDelay):
					case <-ctx.Done():
						return
					}
				}
				result := m.runResult
				if result == nil {
					result = &RunResult{}
				}
				if result.Result != "" && params.OnContent != nil {
					params.OnContent(result.Result)
				}
				m.runs.Add(1)
				select {
				case params.Completed <- result:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return &RunResult{}
}

func newTestHandler() *Handler {
	return NewHandler(nil)
}

func newTestHandlerWithRunner(r Runner) *Handler {
	return NewHandler(r)
}

// waitForTaskStatus polls until every task in h reaches one of the
// expected statuses (or the test times out). Used in tests because the
// per-task driver goroutines live until StopAll is called, so we can't
// rely on h.wg.Wait() to signal end-of-run anymore.
func waitForTaskStatus(t *testing.T, h *Handler, want ...taskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all := true
		h.tasks.Range(func(_ string, tk *task) bool {
			s := tk.loadStatus()
			matched := false
			for _, w := range want {
				if s == w {
					matched = true
					break
				}
			}
			if !matched {
				all = false
				return false
			}
			return true
		})
		if all {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("tasks did not reach status %v in time", want)
}

func TestNewHandlerSharingTasks_SharesTaskControl(t *testing.T) {
	parent := NewHandler(&mockRunner{subAgentNames: []string{"parent-child"}})
	child := NewHandlerSharingTasks(&mockRunner{subAgentNames: []string{"child-child"}}, parent)

	tk := insertTask(parent, "shared-session", "worker", taskRunning)
	ms := &mockSteerable{}
	tk.runtime = ms

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "shared-session", Message: "hello from sibling"})
	result, err := child.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, ms.steered, 1)
	assert.Equal(t, "hello from sibling", ms.steered[0])
}

func TestNewHandlerSharingTasks_UsesOwnRunnerForNewTasks(t *testing.T) {
	parentRunner := &mockRunner{subAgentNames: []string{"parent-child"}}
	childRunner := &mockRunner{subAgentNames: []string{"child-child"}, runResult: &RunResult{Result: "done"}}
	parent := NewHandler(parentRunner)
	child := NewHandlerSharingTasks(childRunner, parent)
	t.Cleanup(child.StopAll)

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "child-child", Task: "do work"})
	result, err := child.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	require.False(t, result.IsError)
	waitForTaskStatus(t, child, taskCompleted)

	assert.Equal(t, 1, child.totalTaskCount())
	assert.Nil(t, parentRunner.lastCtxDone, "shared handler must start tasks through the child runner")
	assert.NotNil(t, childRunner.lastCtxDone)
}

func insertTask(h *Handler, id, agentName string, status taskStatus) *task {
	t := &task{
		sessionID: id,
		agentName: agentName,
		taskDesc:  "test task",
		cancel:    func() {},
		startTime: time.Now(),
	}
	t.status.Store(int32(status))
	h.tasks.Store(id, t)
	return t
}

func makeToolCall(t *testing.T, args any) tools.ToolCall {
	t.Helper()
	b, err := json.Marshal(args)
	require.NoError(t, err)
	return tools.ToolCall{Function: tools.FunctionCall{Arguments: string(b)}}
}

// --- newSessionID ---

func TestNewSessionID_IsUnique(t *testing.T) {
	ids := make(map[string]struct{})
	for range 100 {
		id := newSessionID()
		assert.NotEmpty(t, id)
		_, dup := ids[id]
		assert.False(t, dup, "duplicate session ID: %s", id)
		ids[id] = struct{}{}
	}
}

// --- statusToString ---

func TestStatusToString(t *testing.T) {
	cases := []struct {
		status   taskStatus
		expected string
	}{
		{taskRunning, "running"},
		{taskCompleted, "completed"},
		{taskStopped, "stopped"},
		{taskFailed, "failed"},
		{99, "unknown"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.expected, tc.status.String())
	}
}

// --- runningTaskCount / totalTaskCount ---

func TestTaskCounts(t *testing.T) {
	h := newTestHandler()
	assert.Equal(t, 0, h.runningTaskCount())
	assert.Equal(t, 0, h.totalTaskCount())

	insertTask(h, "t1", "a", taskRunning)
	insertTask(h, "t2", "b", taskRunning)
	insertTask(h, "t3", "c", taskCompleted)
	insertTask(h, "t4", "d", taskFailed)

	assert.Equal(t, 2, h.runningTaskCount())
	assert.Equal(t, 4, h.totalTaskCount())
}

// --- pruneCompleted ---

func TestPruneCompleted(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "run1", "a", taskRunning)
	insertTask(h, "done1", "b", taskCompleted)
	insertTask(h, "done2", "c", taskStopped)
	insertTask(h, "fail1", "d", taskFailed)

	h.pruneCompleted()

	assert.Equal(t, 1, h.totalTaskCount())
	_, exists := h.tasks.Load("run1")
	assert.True(t, exists, "running task should be kept")
	_, exists = h.tasks.Load("done1")
	assert.False(t, exists, "completed task should be pruned")
}

// --- HandleList ---

func TestHandleList_Empty(t *testing.T) {
	h := newTestHandler()
	result, err := h.HandleList(t.Context(), nil, tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No background agents found")
}

func TestHandleList_ShowsTasks(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskRunning)
	insertTask(h, "t2", "writer", taskCompleted)

	result, err := h.HandleList(t.Context(), nil, tools.ToolCall{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "researcher")
	assert.Contains(t, result.Output, "writer")
	assert.Contains(t, result.Output, "running")
	assert.Contains(t, result.Output, "completed")
}

// --- HandleView ---

func TestHandleView_NotFound(t *testing.T) {
	h := newTestHandler()
	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "nonexistent"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "background agent session not found")
}

func TestHandleView_Completed(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "researcher", taskCompleted)
	tk.result = "Here is my research."

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Here is my research.")
	assert.Contains(t, result.Output, "completed")
}

func TestHandleView_Failed(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "researcher", taskFailed)
	tk.errMsg = "model unavailable"

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "task failed")
	assert.Contains(t, result.Output, "model unavailable")
}

func TestHandleView_Running_NoOutputYet(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskRunning)

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "no output yet")
}

func TestHandleView_Running_WithProgress(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "researcher", taskRunning)
	tk.output.WriteString("Partial research so far...")

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Partial research so far...")
	assert.Contains(t, result.Output, "still running")
}

func TestHandleView_Stopped(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskStopped)

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "stopped")
	assert.Contains(t, result.Output, "task was stopped")
}

func TestHandleView_Completed_EmptyResult(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskCompleted)

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "no output")
}

func TestHandleView_OutputBufferTruncated(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "researcher", taskRunning)
	tk.output.WriteString(strings.Repeat("x", maxOutputBytes))
	tk.outputBytes = maxOutputBytes

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "truncated", "should show truncation notice when buffer is full")
	assert.Contains(t, result.Output, "still running")
}

func TestHandleView_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	bad := tools.ToolCall{Function: tools.FunctionCall{Arguments: "not-json"}}
	_, err := h.HandleView(t.Context(), nil, bad)
	require.Error(t, err, "invalid JSON should return an error")
}

func TestHandleView_RepeatedPolling_NoNewOutput(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskRunning)

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})

	// First view should not include poll marker.
	result1, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.NotContains(t, result1.Output, "poll #")

	// Second view with no new output should include poll marker.
	result2, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result2.Output, "poll #2")

	// Third view should show poll #3.
	result3, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result3.Output, "poll #3")

	// Responses should be non-identical.
	assert.NotEqual(t, result2.Output, result3.Output)
}

func TestHandleView_RepeatedPolling_OutputGrows(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "researcher", taskRunning)

	tc := makeToolCall(t, ViewBackgroundAgentArgs{SessionID: "t1"})

	// First view.
	_, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)

	// Second view with no change → poll #2.
	result2, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.Contains(t, result2.Output, "poll #2")

	// Simulate new output arriving.
	tk.outputMu.Lock()
	tk.output.WriteString("new progress")
	tk.outputBytes += len("new progress")
	tk.outputMu.Unlock()

	// Third view should reset the poll counter since output changed.
	result3, err := h.HandleView(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.NotContains(t, result3.Output, "poll #", "poll marker should reset after new output")
	assert.Contains(t, result3.Output, "new progress")
}

// --- HandleStop ---

func TestHandleStop_NotFound(t *testing.T) {
	h := newTestHandler()
	tc := makeToolCall(t, StopBackgroundAgentArgs{SessionID: "ghost"})
	result, err := h.HandleStop(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "background agent session not found")
}

func TestHandleStop_AlreadyCompleted(t *testing.T) {
	h := newTestHandler()
	insertTask(h, "t1", "researcher", taskCompleted)

	tc := makeToolCall(t, StopBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleStop(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "not running")
}

func TestHandleStop_Running(t *testing.T) {
	h := newTestHandler()
	cancelled := false
	tk := insertTask(h, "t1", "researcher", taskRunning)
	tk.cancel = func() { cancelled = true }

	tc := makeToolCall(t, StopBackgroundAgentArgs{SessionID: "t1"})
	result, err := h.HandleStop(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, cancelled)
	assert.Equal(t, taskStopped, tk.loadStatus())
}

func TestHandleStop_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	bad := tools.ToolCall{Function: tools.FunctionCall{Arguments: "not-json"}}
	_, err := h.HandleStop(t.Context(), nil, bad)
	require.Error(t, err, "invalid JSON should return an error")
}

// --- StopAll waits for goroutines ---

func TestStopAll_WaitsForGoroutines(t *testing.T) {
	h := newTestHandler()

	var goroutineExited atomic.Bool
	tk := insertTask(h, "t1", "researcher", taskRunning)
	ctx, cancel := context.WithCancel(t.Context())
	tk.cancel = cancel

	h.wg.Go(func() {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond) // simulate teardown work
		goroutineExited.Store(true)
	})

	h.StopAll()
	assert.True(t, goroutineExited.Load(), "StopAll should wait for goroutine to exit")
}

// --- HandleRun: input validation ---

func TestHandleRun_EmptyAgent(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})
	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "agent name must not be empty")
}

func TestHandleRun_EmptyTask(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})
	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: ""})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "task must not be empty")
}

func TestHandleRun_InvalidSubAgent(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})
	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "nonexistent", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "not in the sub-agents list")
}

func TestHandleRun_NoSubAgents(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: nil})
	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "some-agent", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "no sub-agents configured")
}

func TestHandleRun_ConcurrencyCapEnforced(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})

	for i := range maxConcurrentTasks {
		insertTask(h, "fake"+string(rune('a'+i)), "sub", taskRunning)
	}

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "maximum concurrent")
}

func TestHandleRun_InvalidJSON(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})
	bad := tools.ToolCall{Function: tools.FunctionCall{Arguments: "not-json"}}
	_, err := h.HandleRun(t.Context(), session.New(), bad)
	require.Error(t, err, "invalid JSON should return an error")
}

func TestHandleRun_StartsTask(t *testing.T) {
	r := &mockRunner{
		subAgentNames: []string{"sub"},
		runResult:     &RunResult{Result: "done"},
	}
	h := newTestHandlerWithRunner(r)
	t.Cleanup(h.StopAll)

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: "write a poem"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Session ID:")
	assert.Contains(t, result.Output, "sub")

	waitForTaskStatus(t, h, taskCompleted)

	assert.Equal(t, 1, h.totalTaskCount())
	h.tasks.Range(func(_ string, tk *task) bool {
		select {
		case <-tk.ctx.Done():
			t.Fatal("task lifetime context was canceled after normal completion; completed tasks must remain resumable")
		default:
		}
		return true
	})
}

func TestHandleRun_ProviderError_TaskFails(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{
		subAgentNames: []string{"sub"},
		runResult:     &RunResult{ErrMsg: "model unavailable"},
	})
	t.Cleanup(h.StopAll)

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.False(t, result.IsError, "HandleRun should start successfully before provider error")

	waitForTaskStatus(t, h, taskFailed)

	h.tasks.Range(func(_ string, tk *task) bool {
		assert.NotEmpty(t, tk.errMsg)
		return true
	})
}

func TestHandleRun_WithExpectedOutput(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{
		subAgentNames: []string{"sub"},
		runResult:     &RunResult{Result: "result"},
	})
	t.Cleanup(h.StopAll)

	tc := makeToolCall(t, RunBackgroundAgentArgs{
		Agent:          "sub",
		Task:           "summarize the document",
		ExpectedOutput: "A one-paragraph summary",
	})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	waitForTaskStatus(t, h, taskCompleted)
}

func TestHandleRun_TotalCapAutoPruneAdmits(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{
		subAgentNames: []string{"sub"},
		runResult:     &RunResult{Result: "done"},
	})
	t.Cleanup(h.StopAll)

	for i := range maxTotalTasks {
		insertTask(h, fmt.Sprintf("done%d", i), "sub", taskCompleted)
	}
	assert.Equal(t, maxTotalTasks, h.totalTaskCount())

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.False(t, result.IsError, "task should be admitted after auto-prune of completed tasks")
}

func TestHandleRun_TotalCapExhaustion_ConcurrencyCapFiresFirst(t *testing.T) {
	h := newTestHandlerWithRunner(&mockRunner{subAgentNames: []string{"sub"}})

	for i := range maxConcurrentTasks {
		insertTask(h, fmt.Sprintf("run%d", i), "sub", taskRunning)
	}

	tc := makeToolCall(t, RunBackgroundAgentArgs{Agent: "sub", Task: "do something"})
	result, err := h.HandleRun(t.Context(), session.New(), tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "maximum concurrent",
		"concurrency cap should fire before total cap can be exhausted non-prunably")
}

// --- Concurrent handler access (run with -race) ---

func TestHandler_ConcurrentAccess(t *testing.T) {
	h := newTestHandler()

	for i := range 10 {
		tk := insertTask(h, fmt.Sprintf("task%d", i), "researcher", taskRunning)
		tk.output.WriteString("some progress output")
		tk.outputBytes = len("some progress output")
	}

	viewTCs := make([]tools.ToolCall, 5)
	for i := range 5 {
		viewTCs[i] = makeToolCall(t, ViewBackgroundAgentArgs{SessionID: fmt.Sprintf("task%d", i%10)})
	}
	stopTCs := make([]tools.ToolCall, 3)
	for i := range 3 {
		stopTCs[i] = makeToolCall(t, StopBackgroundAgentArgs{SessionID: fmt.Sprintf("task%d", i)})
	}

	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			_, _ = h.HandleList(t.Context(), nil, tools.ToolCall{})
		})
	}

	for i := range 5 {
		wg.Add(1)
		go func(tc tools.ToolCall) {
			defer wg.Done()
			_, _ = h.HandleView(t.Context(), nil, tc)
		}(viewTCs[i])
	}

	for i := range 3 {
		wg.Add(1)
		go func(tc tools.ToolCall) {
			defer wg.Done()
			_, _ = h.HandleStop(t.Context(), nil, tc)
		}(stopTCs[i])
	}

	wg.Wait()
	assert.LessOrEqual(t, h.runningTaskCount(), 10)
}

// --- Tools ---

func TestNewToolSet_ReturnsFiveTools(t *testing.T) {
	ts := New()
	toolsList, err := ts.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, toolsList, 5)

	names := make([]string, len(toolsList))
	for i, tl := range toolsList {
		names[i] = tl.Name
	}
	assert.Contains(t, names, ToolNameRunBackgroundAgent)
	assert.Contains(t, names, ToolNameListBackgroundAgents)
	assert.Contains(t, names, ToolNameViewBackgroundAgent)
	assert.Contains(t, names, ToolNameStopBackgroundAgent)
	assert.Contains(t, names, ToolNameSendMessageBackgroundAgent)
}

func TestNewToolSet_Instructions(t *testing.T) {
	ts := New()
	instructable, ok := ts.(tools.Instructable)
	require.True(t, ok, "NewToolSet should implement Instructable")

	instructions := instructable.Instructions()
	assert.NotEmpty(t, instructions)
	assert.Contains(t, instructions, "run_background_agent")
	assert.Contains(t, instructions, "list_background_agents")
	assert.Contains(t, instructions, "view_background_agent")
	assert.Contains(t, instructions, "stop_background_agent")
	assert.Contains(t, instructions, "send_message_background_agent")
}

// --- HandleSendMessage ---

// mockSteerable records calls to Steer for assertion in tests.
type mockSteerable struct {
	steered []string
	err     error
}

func (m *mockSteerable) Steer(msg string) error {
	m.steered = append(m.steered, msg)
	return m.err
}

func TestHandleSendMessage_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	bad := tools.ToolCall{Function: tools.FunctionCall{Arguments: "not-json"}}
	_, err := h.HandleSendMessage(t.Context(), nil, bad)
	require.Error(t, err, "invalid JSON should return an error")
}

func TestHandleSendMessage_EmptySessionID(t *testing.T) {
	h := newTestHandler()
	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "  ", Message: "hello"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "session_id must not be empty")
}

func TestHandleSendMessage_EmptyMessage(t *testing.T) {
	h := newTestHandler()
	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "  "})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "message must not be empty")
}

func TestHandleSendMessage_NotFound(t *testing.T) {
	h := newTestHandler()
	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "ghost", Message: "hello"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "background agent session not found")
}

func TestHandleSendMessage_NotRunning(t *testing.T) {
	cases := []struct {
		name   string
		status taskStatus
	}{
		{"stopped", taskStopped},
		{"failed", taskFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHandler()
			insertTask(h, "t1", "sub", c.status)

			tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "hello"})
			result, err := h.HandleSendMessage(t.Context(), nil, tc)
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Output, "cannot receive messages")
			assert.Contains(t, result.Output, c.status.String())
		})
	}
}

func TestHandleSendMessage_CompletedTaskSignalsResume(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "sub", taskCompleted)
	ms := &mockSteerable{}
	tk.runtime = ms
	tk.needsRun = make(chan struct{}, 4)

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "wake up"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Message sent")

	require.Len(t, ms.steered, 1)
	assert.Equal(t, "wake up", ms.steered[0])
	assert.Equal(t, taskRunning, tk.loadStatus(), "status must flip back to running so a new RunStream can start")

	select {
	case <-tk.needsRun:
		// expected: HandleSendMessage signalled the driver.
	case <-time.After(time.Second):
		t.Fatal("HandleSendMessage did not signal needsRun for the resumed task")
	}
}

func TestHandleSendMessage_CompletedTaskNoNeedsRun(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "sub", taskCompleted)
	tk.runtime = &mockSteerable{}
	// needsRun is nil — simulating a runner that never populated the channel.

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "hello"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "runtime not available")
	assert.Equal(t, taskCompleted, tk.loadStatus(), "status must not change when driver wiring is unavailable")
}

func TestHandleSendMessage_RuntimeNotReady(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "sub", taskRunning)
	require.Nil(t, tk.runtime, "precondition: runtime must be nil")

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "hello"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "runtime not available")
}

func TestHandleSendMessage_Success(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "sub", taskRunning)
	ms := &mockSteerable{}
	tk.runtime = ms

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "change direction"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Message sent")
	assert.Contains(t, result.Output, "t1")
	require.Len(t, ms.steered, 1)
	assert.Equal(t, "change direction", ms.steered[0])
}

func TestHandleSendMessage_SteerError(t *testing.T) {
	h := newTestHandler()
	tk := insertTask(h, "t1", "sub", taskRunning)
	ms := &mockSteerable{err: errors.New("queue full")}
	tk.runtime = ms

	tc := makeToolCall(t, SendMessageBackgroundAgentArgs{SessionID: "t1", Message: "hello"})
	result, err := h.HandleSendMessage(t.Context(), nil, tc)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "failed to send message")
	assert.Contains(t, result.Output, "queue full")
}

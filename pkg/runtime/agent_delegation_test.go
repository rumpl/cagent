package runtime

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/tools"
	agenttool "github.com/docker/docker-agent/pkg/tools/builtin/agent"
)

func TestBuildTaskSystemMessage(t *testing.T) {
	t.Run("with expected output", func(t *testing.T) {
		msg := buildTaskSystemMessage("do the thing", "a result", nil, "", "")
		assert.Contains(t, msg, "<task>\ndo the thing\n</task>")
		assert.Contains(t, msg, "<expected_output>\na result\n</expected_output>")
		assert.NotContains(t, msg, "<attached_files>")
		assert.NotContains(t, msg, "background agent")
	})

	t.Run("without expected output", func(t *testing.T) {
		msg := buildTaskSystemMessage("do the thing", "", nil, "", "")
		assert.Contains(t, msg, "<task>\ndo the thing\n</task>")
		assert.NotContains(t, msg, "expected_output")
		assert.NotContains(t, msg, "<attached_files>")
	})

	t.Run("with attached files", func(t *testing.T) {
		msg := buildTaskSystemMessage("do the thing", "", []string{"/abs/foo.go", "/abs/bar.go"}, "", "")
		assert.Contains(t, msg, "<task>\ndo the thing\n</task>")
		assert.Contains(t, msg, "<attached_files>\n- /abs/foo.go\n- /abs/bar.go\n</attached_files>")
	})

	t.Run("with background agent identity", func(t *testing.T) {
		msg := buildTaskSystemMessage("do the thing", "", nil, "sess-42", "orchestrator")
		assert.Contains(t, msg, "You are running as a background agent.")
		assert.Contains(t, msg, "Your session ID is: sess-42")
		assert.Contains(t, msg, "You were started by agent: orchestrator")
		assert.Contains(t, msg, "Other background agents can address you by this session ID")
		assert.Contains(t, msg, "send_message_background_agent")
		assert.Contains(t, msg, "with that agent's session_id")
	})

	t.Run("background agent without parent name", func(t *testing.T) {
		msg := buildTaskSystemMessage("do the thing", "", nil, "sess-99", "")
		assert.Contains(t, msg, "Your session ID is: sess-99")
		assert.NotContains(t, msg, "You were started by agent")
	})
}

func TestAgentNames(t *testing.T) {
	agents := []*agent.Agent{
		agent.New("alpha", ""),
		agent.New("beta", ""),
	}
	assert.Equal(t, []string{"alpha", "beta"}, agentNames(agents))
	assert.Empty(t, agentNames(nil))
}

func TestValidateAgentInList(t *testing.T) {
	agents := []*agent.Agent{
		agent.New("sub1", ""),
		agent.New("sub2", ""),
	}

	t.Run("valid agent returns nil", func(t *testing.T) {
		result := validateAgentInList("root", "sub1", "transfer to", "sub-agents", agents)
		assert.Nil(t, result)
	})

	t.Run("invalid agent with non-empty list", func(t *testing.T) {
		result := validateAgentInList("root", "missing", "transfer to", "sub-agents", agents)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Output, "sub1")
		assert.Contains(t, result.Output, "sub2")
	})

	t.Run("invalid agent with empty list", func(t *testing.T) {
		result := validateAgentInList("root", "missing", "transfer to", "sub-agents", nil)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Output, "No agents are configured")
	})
}

func TestNewSubSession(t *testing.T) {
	parent := session.New(session.WithUserMessage("hello"))
	childAgent := agent.New("worker", "a worker agent",
		agent.WithMaxIterations(10),
	)

	t.Run("basic config", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:           "write tests",
			ExpectedOutput: "passing tests",
			AgentName:      "worker",
			Title:          "Test task",
			ToolsApproved:  true,
		}

		s := newSubSession(parent, cfg, childAgent)

		assert.Equal(t, parent.ID, s.ParentID)
		assert.Equal(t, "Test task", s.Title)
		assert.True(t, s.ToolsApproved)
		assert.False(t, s.SendUserMessage)
		assert.Equal(t, 10, s.MaxIterations)
		// AgentName should NOT be set when PinAgent is false
		assert.Empty(t, s.AgentName)
	})

	t.Run("pin agent", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:      "background work",
			AgentName: "worker",
			Title:     "Background task",
			PinAgent:  true,
		}

		s := newSubSession(parent, cfg, childAgent)

		assert.Equal(t, "worker", s.AgentName)
	})

	t.Run("custom implicit user message", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:                "bump deps",
			AgentName:           "worker",
			Title:               "Skill task",
			ImplicitUserMessage: "Update all Go dependencies",
		}

		s := newSubSession(parent, cfg, childAgent)

		// The implicit user message should be the custom one, not "Please proceed."
		assert.Equal(t, "Update all Go dependencies", s.GetLastUserMessageContent())
	})

	t.Run("default implicit user message", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:      "do work",
			AgentName: "worker",
			Title:     "Task",
		}

		s := newSubSession(parent, cfg, childAgent)

		assert.Equal(t, "Please proceed.", s.GetLastUserMessageContent())
	})

	t.Run("custom system message", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:          "bump deps",
			SystemMessage: "You are a skill sub-agent. Follow these instructions.",
			AgentName:     "worker",
			Title:         "Skill task",
		}

		s := newSubSession(parent, cfg, childAgent)

		// When SystemMessage is set, the default task-based message should not be used.
		// We can verify the user message is still the default.
		assert.Equal(t, "Please proceed.", s.GetLastUserMessageContent())
	})

	t.Run("session id forces session ID and injects identity", func(t *testing.T) {
		cfg := SubSessionConfig{
			Task:            "do work",
			AgentName:       "worker",
			Title:           "Background",
			SessionID:       "pre-assigned-id",
			ParentAgentName: "root",
		}

		s := newSubSession(parent, cfg, childAgent)
		assert.Equal(t, "pre-assigned-id", s.ID)

		msgs := s.GetMessages(childAgent)
		require.NotEmpty(t, msgs)
		var joined strings.Builder
		for _, m := range msgs {
			joined.WriteString(m.Content)
			joined.WriteString("\n")
		}
		assert.Contains(t, joined.String(), "Your session ID is: pre-assigned-id")
		assert.Contains(t, joined.String(), "You were started by agent: root")
	})
}

func TestSubSessionConfig_DefaultValues(t *testing.T) {
	// Verify zero-value SubSessionConfig produces a valid session
	parent := session.New(session.WithUserMessage("hello"))
	childAgent := agent.New("worker", "")

	cfg := SubSessionConfig{
		Task:      "minimal task",
		AgentName: "worker",
		Title:     "Minimal",
	}

	s := newSubSession(parent, cfg, childAgent)

	assert.False(t, s.ToolsApproved)
	assert.False(t, s.SendUserMessage)
	assert.Empty(t, s.AgentName)
}

func TestSubSessionConfig_InheritsAgentLimits(t *testing.T) {
	parent := session.New(session.WithUserMessage("hello"))

	t.Run("with custom limits", func(t *testing.T) {
		childAgent := agent.New("worker", "",
			agent.WithMaxIterations(42),
			agent.WithMaxConsecutiveToolCalls(7),
		)

		cfg := SubSessionConfig{
			Task:      "work",
			AgentName: "worker",
			Title:     "test",
		}

		s := newSubSession(parent, cfg, childAgent)
		assert.Equal(t, 42, s.MaxIterations)
		assert.Equal(t, 7, s.MaxConsecutiveToolCalls)
	})

	t.Run("with zero limits (defaults)", func(t *testing.T) {
		childAgent := agent.New("worker", "")

		cfg := SubSessionConfig{
			Task:      "work",
			AgentName: "worker",
			Title:     "test",
		}

		s := newSubSession(parent, cfg, childAgent)
		assert.Equal(t, 0, s.MaxIterations)
		assert.Equal(t, 0, s.MaxConsecutiveToolCalls)
	})
}

func TestSubSessionInheritsAttachedFiles(t *testing.T) {
	parent := session.New(session.WithUserMessage("hello"))
	parent.AddAttachedFile("/abs/foo.go")
	parent.AddAttachedFile("/abs/bar.go")
	parent.AddAttachedFile("/abs/foo.go") // duplicate, should be ignored

	childAgent := agent.New("worker", "")
	cfg := SubSessionConfig{
		Task:      "refactor",
		AgentName: "worker",
		Title:     "Refactor",
	}

	s := newSubSession(parent, cfg, childAgent)

	// Child session inherits parent's attached files (deduplicated, ordered).
	assert.Equal(t, []string{"/abs/foo.go", "/abs/bar.go"}, s.AttachedFilesSnapshot())

	// The system message lists them so the sub-agent sees them up-front.
	sysMsg := s.GetMessages(childAgent)
	require.NotEmpty(t, sysMsg)
	var joined strings.Builder
	for _, m := range sysMsg {
		joined.WriteString(m.Content)
		joined.WriteString("\n")
	}
	assert.Contains(t, joined.String(), "<attached_files>\n- /abs/foo.go\n- /abs/bar.go\n</attached_files>")
}

func TestSubSessionWithoutAttachedFilesOmitsBlock(t *testing.T) {
	parent := session.New(session.WithUserMessage("hello"))
	childAgent := agent.New("worker", "")
	cfg := SubSessionConfig{
		Task:      "refactor",
		AgentName: "worker",
		Title:     "Refactor",
	}

	s := newSubSession(parent, cfg, childAgent)
	assert.Empty(t, s.AttachedFilesSnapshot())

	msgs := s.GetMessages(childAgent)
	require.NotEmpty(t, msgs)
	for _, m := range msgs {
		assert.NotContains(t, m.Content, "<attached_files>")
	}
}

// TestRunAgent_ResumeAfterIdleSteer verifies that send_message_background_agent
// can wake an idle (completed) background task by calling the Resume closure
// captured via RunParams.OnRuntimeReady. The first RunStream returns after a
// single turn; a steer message is then enqueued and Resume is invoked,
// causing a second RunStream that delivers the steered message to the model.
func TestRunAgent_ResumeAfterIdleSteer(t *testing.T) {
	t.Parallel()

	// Two streams: the first turn produces a short "done" message and
	// stops; the second turn (triggered by the steer + resume) produces
	// "got steered" and stops.
	stream1 := newStreamBuilder().
		AddContent("done").
		AddStopWithUsage(5, 3).
		Build()
	stream2 := newStreamBuilder().
		AddContent("got steered").
		AddStopWithUsage(5, 3).
		Build()

	prov := &messageRecordingProvider{
		id:      "test/mock-model",
		streams: []*mockStream{stream1, stream2},
	}

	worker := agent.New("worker", "You are the worker", agent.WithModel(prov))
	root := agent.New("root", "You are the root", agent.WithModel(prov), agent.WithSubAgents(worker))
	tm := team.New(team.WithAgents(root, worker))

	rt, err := NewLocalRuntime(tm,
		WithCurrentAgent("root"),
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
	)
	require.NoError(t, err)

	parent := session.New(session.WithUserMessage("kick off"))

	var (
		mu        sync.Mutex
		taskRt    agenttool.TaskRuntime
		readyOnce sync.Once
		readyChan = make(chan struct{})
		collected strings.Builder
	)

	result := rt.RunAgent(t.Context(), agenttool.RunParams{
		AgentName:     "worker",
		Task:          "do the thing",
		ParentSession: parent,
		OnContent: func(c string) {
			mu.Lock()
			collected.WriteString(c)
			mu.Unlock()
		},
		OnRuntimeReady: func(rt agenttool.TaskRuntime) {
			taskRt = rt
			readyOnce.Do(func() { close(readyChan) })
		},
	})

	require.Empty(t, result.ErrMsg, "first turn must complete cleanly")
	assert.Equal(t, "done", result.Result, "first turn returns the model's last message")

	// OnRuntimeReady must have fired during RunAgent.
	select {
	case <-readyChan:
	default:
		t.Fatal("OnRuntimeReady was not invoked")
	}
	require.NotNil(t, taskRt.Steerable, "Steerable must be set")
	require.NotNil(t, taskRt.Resume, "Resume must be set")

	// Simulate send_message_background_agent on an idle task: enqueue a
	// steer message, then invoke Resume to re-drive the same sub-session.
	require.NoError(t, taskRt.Steerable.Steer("please rewind"))

	resumeResult := taskRt.Resume(t.Context())
	require.Empty(t, resumeResult.ErrMsg, "resume must complete cleanly")
	assert.Equal(t, "got steered", resumeResult.Result, "resume returns the second turn's last message")

	// The second model call must have received the steered content.
	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.GreaterOrEqual(t, len(prov.recordedMessages), 2, "model must be called at least twice")

	var foundSteer bool
	for _, m := range prov.recordedMessages[len(prov.recordedMessages)-1] {
		if strings.Contains(m.Content, "please rewind") {
			foundSteer = true
			break
		}
	}
	assert.True(t, foundSteer, "second model call must include the steered content")
}

func TestBackgroundAgentChildRuntimeCanMessageSibling(t *testing.T) {
	t.Parallel()

	prov := &blockingProvider{id: "test/blocking-model"}
	alice := agent.New("alice", "Alice worker", agent.WithModel(prov))
	bob := agent.New("bob", "Bob worker", agent.WithModel(prov))
	root := agent.New("root", "Root coordinator", agent.WithModel(prov), agent.WithSubAgents(alice, bob))
	tm := team.New(team.WithAgents(root, alice, bob))

	rt, err := NewLocalRuntime(tm,
		WithCurrentAgent("root"),
		WithSessionCompaction(false),
		WithModelStore(mockModelStore{}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })

	started := make(chan BackgroundAgentStart, 2)
	rt.OnBackgroundAgentStarted(func(start BackgroundAgentStart) {
		started <- start
	})

	parent := session.New(session.WithUserMessage("start both workers"))
	startAgent := func(name string) {
		t.Helper()
		args, err := json.Marshal(agenttool.RunBackgroundAgentArgs{Agent: name, Task: "wait for messages"})
		require.NoError(t, err)
		result, err := rt.bgAgents.HandleRun(t.Context(), parent, tools.ToolCall{Function: tools.FunctionCall{Arguments: string(args)}})
		require.NoError(t, err)
		require.False(t, result.IsError, result.Output)
	}

	startAgent("alice")
	startAgent("bob")

	starts := make(map[string]BackgroundAgentStart)
	for len(starts) < 2 {
		select {
		case start := <-started:
			starts[start.AgentName] = start
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for background agents to start; got %v", starts)
		}
	}

	aliceRuntime, ok := starts["alice"].Runtime.(*LocalRuntime)
	require.True(t, ok, "expected alice runtime to be local")

	select {
	case ev := <-starts["alice"].Events:
		require.IsType(t, &TeamInfoEvent{}, ev)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded background agent events")
	}

	args, err := json.Marshal(agenttool.SendMessageBackgroundAgentArgs{
		SessionID: starts["bob"].SessionID,
		Message:   "ping from alice",
	})
	require.NoError(t, err)
	result, err := aliceRuntime.bgAgents.HandleSendMessage(t.Context(), starts["alice"].Session, tools.ToolCall{Function: tools.FunctionCall{Arguments: string(args)}})
	require.NoError(t, err)
	require.False(t, result.IsError, result.Output)
}

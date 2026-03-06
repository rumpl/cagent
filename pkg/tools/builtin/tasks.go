package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/path"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameCreateTask       = "create_task"
	ToolNameGetTask          = "get_task"
	ToolNameUpdateTask       = "update_task"
	ToolNameDeleteTask       = "delete_task"
	ToolNameListTasks        = "list_tasks"
	ToolNameNextTask         = "next_task"
	ToolNameAddDependency    = "add_dependency"
	ToolNameRemoveDependency = "remove_dependency"
)

type TaskPriority string

const (
	PriorityCritical TaskPriority = "critical"
	PriorityHigh     TaskPriority = "high"
	PriorityMedium   TaskPriority = "medium"
	PriorityLow      TaskPriority = "low"
)

var priorityOrder = map[TaskPriority]int{
	PriorityCritical: 0,
	PriorityHigh:     1,
	PriorityMedium:   2,
	PriorityLow:      3,
}

func validPriority(p string) bool {
	_, ok := priorityOrder[TaskPriority(p)]
	return ok
}

type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
	StatusBlocked    TaskStatus = "blocked"
)

func validStatus(s string) bool {
	switch TaskStatus(s) {
	case StatusPending, StatusInProgress, StatusDone, StatusBlocked:
		return true
	}
	return false
}

type Task struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Priority     TaskPriority `json:"priority"`
	Status       TaskStatus   `json:"status"`
	Dependencies []string     `json:"dependencies"`
	CreatedAt    string       `json:"createdAt"`
	UpdatedAt    string       `json:"updatedAt"`
}

// TaskWithStatus pairs a task with its effective status, which accounts
// for the state of its dependencies.
type TaskWithStatus struct {
	Task
	EffectiveStatus TaskStatus `json:"effectiveStatus"`
}

// Sentinel errors returned by TaskStorage implementations.
var (
	ErrTaskNotFound       = errors.New("task not found")
	ErrDependencyNotFound = errors.New("dependency task not found")
	ErrDependencyCycle    = errors.New("dependency would create a cycle")
	ErrDuplicateDependency = errors.New("dependency already exists")
)

// TaskStorage defines the storage layer for task items.
//
// Implementations are responsible for dependency integrity: Create and
// Update must verify that every referenced dependency exists and that
// the resulting graph is acyclic. Delete must remove the deleted task
// from all other tasks' dependency lists.
type TaskStorage interface {
	// Create persists a new task.
	// Returns ErrDependencyNotFound or ErrDependencyCycle on invalid dependencies.
	Create(task Task) error

	// Get returns a task and its effective status.
	// Returns ErrTaskNotFound if the ID does not exist.
	Get(id string) (TaskWithStatus, error)

	// List returns every task with its effective status.
	List() ([]TaskWithStatus, error)

	// Update modifies an existing task.
	// Returns ErrTaskNotFound if the task does not exist,
	// ErrDependencyNotFound or ErrDependencyCycle on invalid dependencies.
	Update(task Task) error

	// Delete removes a task and cleans up references to it in other
	// tasks' dependency lists.
	// Returns ErrTaskNotFound if the ID does not exist.
	Delete(id string) error
}

// ---------------------------------------------------------------------------
// FileTaskStorage — file-backed implementation
// ---------------------------------------------------------------------------

type taskStore struct {
	Tasks map[string]Task `json:"tasks"`
}

// FileTaskStorage is a file-backed, concurrency-safe implementation of TaskStorage.
type FileTaskStorage struct {
	mu       sync.Mutex
	filePath string
}

// NewFileTaskStorage creates a FileTaskStorage that reads/writes to the given path.
func NewFileTaskStorage(filePath string) *FileTaskStorage {
	return &FileTaskStorage{filePath: filePath}
}

func (s *FileTaskStorage) load() map[string]Task {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return make(map[string]Task)
	}
	var store taskStore
	if err := json.Unmarshal(data, &store); err != nil {
		return make(map[string]Task)
	}
	if store.Tasks == nil {
		return make(map[string]Task)
	}
	return store.Tasks
}

func (s *FileTaskStorage) save(tasks map[string]Task) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return fmt.Errorf("creating storage directory: %w", err)
	}
	data, err := json.MarshalIndent(taskStore{Tasks: tasks}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling task store: %w", err)
	}
	return os.WriteFile(s.filePath, data, 0o644)
}

func (s *FileTaskStorage) validateDeps(tasks map[string]Task, taskID string, deps []string) error {
	for _, depID := range deps {
		if _, ok := tasks[depID]; !ok {
			return fmt.Errorf("%w: %s", ErrDependencyNotFound, depID)
		}
	}
	if hasCycle(tasks, taskID, deps) {
		return ErrDependencyCycle
	}
	return nil
}

func (s *FileTaskStorage) Create(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.load()
	if err := s.validateDeps(tasks, task.ID, task.Dependencies); err != nil {
		return err
	}
	tasks[task.ID] = task
	return s.save(tasks)
}

func (s *FileTaskStorage) Get(id string) (TaskWithStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.load()
	task, ok := tasks[id]
	if !ok {
		return TaskWithStatus{}, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	return TaskWithStatus{
		Task:            task,
		EffectiveStatus: effectiveStatus(task, tasks),
	}, nil
}

func (s *FileTaskStorage) List() ([]TaskWithStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.load()
	result := make([]TaskWithStatus, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, TaskWithStatus{
			Task:            task,
			EffectiveStatus: effectiveStatus(task, tasks),
		})
	}
	return result, nil
}

func (s *FileTaskStorage) Update(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.load()
	if _, ok := tasks[task.ID]; !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, task.ID)
	}
	if err := s.validateDeps(tasks, task.ID, task.Dependencies); err != nil {
		return err
	}
	tasks[task.ID] = task
	return s.save(tasks)
}

func (s *FileTaskStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.load()
	if _, ok := tasks[id]; !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}

	// Remove the deleted task from other tasks' dependency lists.
	for otherID, task := range tasks {
		if otherID == id {
			continue
		}
		filtered := slices.DeleteFunc(slices.Clone(task.Dependencies), func(d string) bool {
			return d == id
		})
		if len(filtered) != len(task.Dependencies) {
			task.Dependencies = filtered
			tasks[otherID] = task
		}
	}

	delete(tasks, id)
	return s.save(tasks)
}

// ---------------------------------------------------------------------------
// TasksTool
// ---------------------------------------------------------------------------

// TaskOption is a functional option for configuring a TasksTool.
type TaskOption func(*TasksTool)

// WithTaskStorage sets a custom storage implementation for the TasksTool.
// A nil value is silently ignored, keeping the default storage.
func WithTaskStorage(storage TaskStorage) TaskOption {
	return func(t *TasksTool) {
		if storage != nil {
			t.storage = storage
		}
	}
}

type TasksTool struct {
	storage  TaskStorage
	basePath string
}

var (
	_ tools.ToolSet      = (*TasksTool)(nil)
	_ tools.Instructable = (*TasksTool)(nil)
)

func NewTasksTool(storagePath string, opts ...TaskOption) *TasksTool {
	t := &TasksTool{
		storage:  NewFileTaskStorage(storagePath),
		basePath: filepath.Dir(storagePath),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *TasksTool) Instructions() string {
	return `## Using the Tasks Tools

Persistent task management with priorities (critical > high > medium > low), statuses (pending, in_progress, done, blocked), and dependencies.

Tasks are saved to a JSON file and survive across sessions. A task is automatically blocked if any dependency is not done.

Workflow: create_task → list_tasks/next_task → update_task as work progresses. Use add_dependency/remove_dependency to manage ordering.`
}

// ---------------------------------------------------------------------------
// Pure helpers (no storage access)
// ---------------------------------------------------------------------------

func effectiveStatus(task Task, tasks map[string]Task) TaskStatus {
	if task.Status == StatusDone {
		return StatusDone
	}
	for _, depID := range task.Dependencies {
		dep, ok := tasks[depID]
		if ok && dep.Status != StatusDone {
			return StatusBlocked
		}
	}
	return task.Status
}

func hasCycle(tasks map[string]Task, startID string, deps []string) bool {
	visited := make(map[string]bool)
	stack := make([]string, len(deps))
	copy(stack, deps)
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == startID {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		if task, ok := tasks[current]; ok {
			stack = append(stack, task.Dependencies...)
		}
	}
	return false
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (t *TasksTool) resolveDescription(description, filePath string) (string, error) {
	if filePath != "" {
		validatedPath, err := path.ValidatePathInDirectory(filePath, t.basePath)
		if err != nil {
			return "", fmt.Errorf("invalid file path: %w", err)
		}
		data, err := os.ReadFile(validatedPath)
		if err != nil {
			return "", fmt.Errorf("reading file %s: %w", validatedPath, err)
		}
		return string(data), nil
	}
	return description, nil
}

func sortTasks(tasks []TaskWithStatus) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if (a.EffectiveStatus == StatusBlocked) != (b.EffectiveStatus == StatusBlocked) {
			return a.EffectiveStatus != StatusBlocked
		}
		pa, pb := priorityOrder[a.Priority], priorityOrder[b.Priority]
		if pa != pb {
			return pa < pb
		}
		return a.CreatedAt < b.CreatedAt
	})
}

// storageError translates a storage error into a tool-call error result,
// returning nil when the error should be surfaced as a Go error instead.
func storageError(err error) *tools.ToolCallResult {
	if errors.Is(err, ErrTaskNotFound) ||
		errors.Is(err, ErrDependencyNotFound) ||
		errors.Is(err, ErrDependencyCycle) ||
		errors.Is(err, ErrDuplicateDependency) {
		return tools.ResultError(err.Error())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tool argument types
// ---------------------------------------------------------------------------

type CreateTaskArgs struct {
	Title        string   `json:"title" jsonschema:"Short title for the task"`
	Description  string   `json:"description,omitempty" jsonschema:"Task description (ignored if path is given)"`
	Path         string   `json:"path,omitempty" jsonschema:"Path to a markdown file whose content becomes the task description"`
	Priority     string   `json:"priority,omitempty" jsonschema:"Priority: critical, high, medium (default), or low"`
	Dependencies []string `json:"dependencies,omitempty" jsonschema:"IDs of tasks that must be completed before this one"`
}

type GetTaskArgs struct {
	ID string `json:"id" jsonschema:"Task ID"`
}

type UpdateTaskArgs struct {
	ID           string   `json:"id" jsonschema:"Task ID to update"`
	Title        string   `json:"title,omitempty" jsonschema:"New title"`
	Description  string   `json:"description,omitempty" jsonschema:"New description"`
	Path         string   `json:"path,omitempty" jsonschema:"Read new description from this file"`
	Priority     string   `json:"priority,omitempty" jsonschema:"New priority: critical, high, medium, or low"`
	Status       string   `json:"status,omitempty" jsonschema:"New status: pending, in_progress, done, or blocked"`
	Dependencies []string `json:"dependencies,omitempty" jsonschema:"Replace dependency list with these task IDs"`
}

type DeleteTaskArgs struct {
	ID string `json:"id" jsonschema:"Task ID to delete"`
}

type ListTasksArgs struct {
	Status   string `json:"status,omitempty" jsonschema:"Filter by effective status: pending, in_progress, done, blocked"`
	Priority string `json:"priority,omitempty" jsonschema:"Filter by priority level: critical, high, medium, low"`
}

type AddDependencyArgs struct {
	TaskID      string `json:"taskId" jsonschema:"The task that depends on another"`
	DependsOnID string `json:"dependsOnId" jsonschema:"The task that must be completed first"`
}

type RemoveDependencyArgs struct {
	TaskID      string `json:"taskId" jsonschema:"The task to remove the dependency from"`
	DependsOnID string `json:"dependsOnId" jsonschema:"The dependency to remove"`
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

func (t *TasksTool) createTask(_ context.Context, params CreateTaskArgs) (*tools.ToolCallResult, error) {
	desc, err := t.resolveDescription(params.Description, params.Path)
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}

	priority := TaskPriority(params.Priority)
	if params.Priority == "" {
		priority = PriorityMedium
	} else if !validPriority(params.Priority) {
		return tools.ResultError(fmt.Sprintf("invalid priority: %s", params.Priority)), nil
	}

	deps := params.Dependencies
	if deps == nil {
		deps = []string{}
	}

	task := Task{
		ID:           uuid.New().String(),
		Title:        params.Title,
		Description:  desc,
		Priority:     priority,
		Status:       StatusPending,
		Dependencies: deps,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}

	if err := t.storage.Create(task); err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}

	return taskResult(task), nil
}

func (t *TasksTool) getTask(_ context.Context, params GetTaskArgs) (*tools.ToolCallResult, error) {
	tws, err := t.storage.Get(params.ID)
	if err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}
	return taskWithStatusResult(tws), nil
}

func (t *TasksTool) updateTask(_ context.Context, params UpdateTaskArgs) (*tools.ToolCallResult, error) {
	tws, err := t.storage.Get(params.ID)
	if err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}
	task := tws.Task

	if params.Title != "" {
		task.Title = params.Title
	}
	if params.Path != "" || params.Description != "" {
		desc, err := t.resolveDescription(params.Description, params.Path)
		if err != nil {
			return tools.ResultError(err.Error()), nil
		}
		task.Description = desc
	}
	if params.Priority != "" {
		if !validPriority(params.Priority) {
			return tools.ResultError(fmt.Sprintf("invalid priority: %s", params.Priority)), nil
		}
		task.Priority = TaskPriority(params.Priority)
	}
	if params.Status != "" {
		if !validStatus(params.Status) {
			return tools.ResultError(fmt.Sprintf("invalid status: %s", params.Status)), nil
		}
		task.Status = TaskStatus(params.Status)
	}
	if params.Dependencies != nil {
		task.Dependencies = params.Dependencies
	}

	task.UpdatedAt = now()

	if err := t.storage.Update(task); err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}

	return taskResult(task), nil
}

func (t *TasksTool) deleteTask(_ context.Context, params DeleteTaskArgs) (*tools.ToolCallResult, error) {
	if err := t.storage.Delete(params.ID); err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}

	out, err := json.MarshalIndent(map[string]string{"deleted": params.ID}, "", "  ")
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}
	return &tools.ToolCallResult{Output: string(out)}, nil
}

func (t *TasksTool) listTasks(_ context.Context, params ListTasksArgs) (*tools.ToolCallResult, error) {
	tasks, err := t.storage.List()
	if err != nil {
		return nil, err
	}

	if params.Status != "" {
		tasks = slices.DeleteFunc(tasks, func(tw TaskWithStatus) bool {
			return string(tw.EffectiveStatus) != params.Status
		})
	}
	if params.Priority != "" {
		tasks = slices.DeleteFunc(tasks, func(tw TaskWithStatus) bool {
			return string(tw.Priority) != params.Priority
		})
	}

	sortTasks(tasks)

	out, err := json.Marshal(tasks)
	if err != nil {
		return tools.ResultError(err.Error()), nil
	}
	return &tools.ToolCallResult{Output: string(out)}, nil
}

func (t *TasksTool) nextTask(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
	tasks, err := t.storage.List()
	if err != nil {
		return nil, err
	}
	sortTasks(tasks)

	for _, task := range tasks {
		if task.EffectiveStatus != StatusBlocked && task.EffectiveStatus != StatusDone {
			out, err := json.Marshal(task)
			if err != nil {
				return tools.ResultError(err.Error()), nil
			}
			return &tools.ToolCallResult{Output: string(out)}, nil
		}
	}

	return &tools.ToolCallResult{
		Output: "No actionable tasks. Everything is either done or blocked.",
	}, nil
}

func (t *TasksTool) addDependency(_ context.Context, params AddDependencyArgs) (*tools.ToolCallResult, error) {
	tws, err := t.storage.Get(params.TaskID)
	if err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}
	task := tws.Task

	if slices.Contains(task.Dependencies, params.DependsOnID) {
		return tools.ResultError(ErrDuplicateDependency.Error()), nil
	}

	task.Dependencies = append(task.Dependencies, params.DependsOnID)
	task.UpdatedAt = now()

	if err := t.storage.Update(task); err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}

	return taskResult(task), nil
}

func (t *TasksTool) removeDependency(_ context.Context, params RemoveDependencyArgs) (*tools.ToolCallResult, error) {
	tws, err := t.storage.Get(params.TaskID)
	if err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}
	task := tws.Task

	task.Dependencies = slices.DeleteFunc(slices.Clone(task.Dependencies), func(d string) bool {
		return d == params.DependsOnID
	})
	task.UpdatedAt = now()

	if err := t.storage.Update(task); err != nil {
		if r := storageError(err); r != nil {
			return r, nil
		}
		return nil, err
	}

	return taskResult(task), nil
}

// ---------------------------------------------------------------------------
// Result helpers
// ---------------------------------------------------------------------------

func taskResult(task Task) *tools.ToolCallResult {
	out, err := json.Marshal(task)
	if err != nil {
		return tools.ResultError(err.Error())
	}
	return &tools.ToolCallResult{Output: string(out)}
}

func taskWithStatusResult(tws TaskWithStatus) *tools.ToolCallResult {
	out, err := json.Marshal(tws)
	if err != nil {
		return tools.ResultError(err.Error())
	}
	return &tools.ToolCallResult{Output: string(out)}
}

func boolPtr(b bool) *bool { return &b }

func (t *TasksTool) Tools(_ context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:        ToolNameCreateTask,
			Category:    "tasks",
			Description: "Create a new task. Provide a title and either a description or a path to a markdown file whose content will be used as the description. Optionally set priority and dependencies on other task IDs.",
			Parameters:  tools.MustSchemaFor[CreateTaskArgs](),
			Handler:     tools.NewHandler(t.createTask),
			Annotations: tools.ToolAnnotations{
				Title: "Create Task",
			},
		},
		{
			Name:        ToolNameGetTask,
			Category:    "tasks",
			Description: "Get full details of a single task by ID, including its effective status (blocked if any dependency is not done).",
			Parameters:  tools.MustSchemaFor[GetTaskArgs](),
			Handler:     tools.NewHandler(t.getTask),
			Annotations: tools.ToolAnnotations{
				Title:        "Get Task",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameUpdateTask,
			Category:    "tasks",
			Description: "Update fields of an existing task. You can change title, description (or path to re-read from file), priority, status, and dependencies.",
			Parameters:  tools.MustSchemaFor[UpdateTaskArgs](),
			Handler:     tools.NewHandler(t.updateTask),
			Annotations: tools.ToolAnnotations{
				Title: "Update Task",
			},
		},
		{
			Name:        ToolNameDeleteTask,
			Category:    "tasks",
			Description: "Delete a task by ID. Also removes it from other tasks' dependency lists.",
			Parameters:  tools.MustSchemaFor[DeleteTaskArgs](),
			Handler:     tools.NewHandler(t.deleteTask),
			Annotations: tools.ToolAnnotations{
				Title:           "Delete Task",
				DestructiveHint: boolPtr(true),
			},
		},
		{
			Name:        ToolNameListTasks,
			Category:    "tasks",
			Description: "List all tasks, sorted by priority (critical first) with blocked tasks last. Optionally filter by status or priority.",
			Parameters:  tools.MustSchemaFor[ListTasksArgs](),
			Handler:     tools.NewHandler(t.listTasks),
			Annotations: tools.ToolAnnotations{
				Title:        "List Tasks",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameNextTask,
			Category:    "tasks",
			Description: strings.TrimSpace("Get the highest-priority actionable task — one that is not blocked and not done. Great for asking 'what should I work on next?'"),
			Handler:     t.nextTask,
			Annotations: tools.ToolAnnotations{
				Title:        "Next Task",
				ReadOnlyHint: true,
			},
		},
		{
			Name:        ToolNameAddDependency,
			Category:    "tasks",
			Description: "Add a dependency: taskId will be blocked until dependsOnId is done.",
			Parameters:  tools.MustSchemaFor[AddDependencyArgs](),
			Handler:     tools.NewHandler(t.addDependency),
			Annotations: tools.ToolAnnotations{
				Title: "Add Dependency",
			},
		},
		{
			Name:        ToolNameRemoveDependency,
			Category:    "tasks",
			Description: "Remove a dependency from a task.",
			Parameters:  tools.MustSchemaFor[RemoveDependencyArgs](),
			Handler:     tools.NewHandler(t.removeDependency),
			Annotations: tools.ToolAnnotations{
				Title: "Remove Dependency",
			},
		},
	}, nil
}

package context

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// Event represents a pipeline event that can be streamed to the UI.
type Event struct {
	Type    string `json:"type"`
	Agent   string `json:"agent,omitempty"`
	Message string `json:"message,omitempty"`
	File    string `json:"file,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// SharedContext is the central state shared across all agents
type SharedContext struct {
	mu sync.RWMutex

	// Core data
	UserPrompt       string                   `json:"user_prompt"`
	Blueprint        *types.Blueprint         `json:"blueprint,omitempty"`
	TaskGraph        []*types.Task            `json:"task_graph,omitempty"`
	GeneratedFiles   map[string]string        `json:"generated_files"`
	ReviewNotes      []*types.ReviewNote      `json:"review_notes,omitempty"`
	TestResults      []*types.TestResult      `json:"test_results,omitempty"`
	IterationHistory []*types.IterationRecord `json:"iteration_history,omitempty"`
	ChainOfThought   []*types.ThoughtStep     `json:"chain_of_thought,omitempty"`

	// Metadata
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ProjectName string    `json:"project_name,omitempty"`
	OutputDir   string    `json:"output_dir,omitempty"`

	// EventBus is an optional channel for streaming pipeline events to the UI.
	// It is not serialised; it is only used during a live run.
	EventBus chan<- Event `json:"-"`
}

// New creates a new SharedContext with default values
func New(prompt string) *SharedContext {
	return &SharedContext{
		UserPrompt:     prompt,
		GeneratedFiles: make(map[string]string),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// SetBlueprint updates the blueprint (thread-safe)
func (c *SharedContext) SetBlueprint(bp *types.Blueprint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Blueprint = bp
	c.UpdatedAt = time.Now()
}

// GetBlueprint returns the blueprint (thread-safe)
func (c *SharedContext) GetBlueprint() *types.Blueprint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Blueprint
}

// SetTaskGraph updates the task graph (thread-safe)
func (c *SharedContext) SetTaskGraph(tasks []*types.Task) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TaskGraph = tasks
	c.UpdatedAt = time.Now()
}

// GetTaskGraph returns the task graph (thread-safe)
func (c *SharedContext) GetTaskGraph() []*types.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TaskGraph
}

// SetFile sets a generated file (thread-safe)
func (c *SharedContext) SetFile(path, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.GeneratedFiles[path] = content
	c.UpdatedAt = time.Now()
}

// GetFile returns a generated file's content (thread-safe)
func (c *SharedContext) GetFile(path string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	content, ok := c.GeneratedFiles[path]
	return content, ok
}

// GetAllFiles returns all generated files (thread-safe)
func (c *SharedContext) GetAllFiles() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]string, len(c.GeneratedFiles))
	for k, v := range c.GeneratedFiles {
		result[k] = v
	}
	return result
}

// AddReviewNote adds a review note (thread-safe)
func (c *SharedContext) AddReviewNote(note *types.ReviewNote) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ReviewNotes = append(c.ReviewNotes, note)
	c.UpdatedAt = time.Now()
}

// GetReviewNotes returns all review notes (thread-safe)
func (c *SharedContext) GetReviewNotes() []*types.ReviewNote {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ReviewNotes
}

// ClearReviewNotes clears review notes (thread-safe)
func (c *SharedContext) ClearReviewNotes() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ReviewNotes = nil
	c.UpdatedAt = time.Now()
}

// HasCriticalIssues returns true if any review note is critical
func (c *SharedContext) HasCriticalIssues() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, note := range c.ReviewNotes {
		if note.Severity == types.SeverityCritical {
			return true
		}
	}
	return false
}

// AddTestResult adds a test result (thread-safe)
func (c *SharedContext) AddTestResult(result *types.TestResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TestResults = append(c.TestResults, result)
	c.UpdatedAt = time.Now()
}

// AddIterationRecord adds an iteration record (thread-safe)
func (c *SharedContext) AddIterationRecord(record *types.IterationRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.IterationHistory = append(c.IterationHistory, record)
	c.UpdatedAt = time.Now()
}

// GetLatestFeedback returns the most recent user feedback
func (c *SharedContext) GetLatestFeedback() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.IterationHistory) == 0 {
		return ""
	}
	return c.IterationHistory[len(c.IterationHistory)-1].Feedback
}

// AddThought adds a chain-of-thought step (thread-safe)
func (c *SharedContext) AddThought(agent types.AgentType, step int, thought, action string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ChainOfThought = append(c.ChainOfThought, &types.ThoughtStep{
		Agent:   agent,
		Step:    step,
		Thought: thought,
		Action:  action,
	})
}

// GetChainOfThought returns all thought steps (thread-safe)
func (c *SharedContext) GetChainOfThought() []*types.ThoughtStep {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ChainOfThought
}

// Serialize returns the shared context as JSON
func (c *SharedContext) Serialize() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.MarshalIndent(c, "", "  ")
}

// UpdateTaskStatus updates a task's status by ID
func (c *SharedContext) UpdateTaskStatus(taskID string, status types.TaskStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, task := range c.TaskGraph {
		if task.ID == taskID {
			task.Status = status
			c.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("task %q not found", taskID)
}

// GetPendingTasks returns tasks that are ready to execute (no unmet dependencies)
func (c *SharedContext) GetPendingTasks() []*types.Task {
	c.mu.RLock()
	defer c.mu.RUnlock()

	completed := make(map[string]bool)
	for _, t := range c.TaskGraph {
		if t.Status == types.TaskCompleted {
			completed[t.ID] = true
		}
	}

	var pending []*types.Task
	for _, t := range c.TaskGraph {
		if t.Status != types.TaskPending {
			continue
		}
		ready := true
		for _, dep := range t.Dependencies {
			if !completed[dep] {
				ready = false
				break
			}
		}
		if ready {
			pending = append(pending, t)
		}
	}
	return pending
}

// Publish sends an event to the EventBus if one is registered.
// It is non-blocking: if the channel is full the event is dropped.
func (c *SharedContext) Publish(evt Event) {
	c.mu.RLock()
	bus := c.EventBus
	c.mu.RUnlock()
	if bus == nil {
		return
	}
	select {
	case bus <- evt:
	default:
	}
}

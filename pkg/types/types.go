package types

import "time"

// ReasoningStrategy defines the type of reasoning used by an agent
type ReasoningStrategy string

const (
	ReasoningCoT   ReasoningStrategy = "cot"
	ReasoningToT   ReasoningStrategy = "tot"
	ReasoningReAct ReasoningStrategy = "react"
)

// AgentType identifies which agent is running
type AgentType string

const (
	AgentOrchestrator AgentType = "orchestrator"
	AgentArchitect    AgentType = "architect"
	AgentPlanner      AgentType = "planner"
	AgentBuilder      AgentType = "builder"
	AgentReviewer     AgentType = "reviewer"
	AgentTester       AgentType = "tester"
	AgentIterator     AgentType = "iterator"
)

// Blueprint represents the system architecture designed by the Architect agent
type Blueprint struct {
	TechStack        TechStack     `json:"tech_stack"`
	FolderStructure  []FolderItem  `json:"folder_structure"`
	DatabaseSchema   []Table       `json:"database_schema"`
	APIEndpoints     []APIEndpoint `json:"api_endpoints"`
	AuthStrategy     AuthStrategy  `json:"auth_strategy"`
	DeploymentConfig DeployConfig  `json:"deployment_config"`
}

type TechStack struct {
	Language   string   `json:"language"`
	Framework  string   `json:"framework"`
	Database   string   `json:"database"`
	Cache      string   `json:"cache,omitempty"`
	Queue      string   `json:"queue,omitempty"`
	Frontend   string   `json:"frontend,omitempty"`
	Deployment string   `json:"deployment"`
	Libraries  []string `json:"libraries,omitempty"`
}

type FolderItem struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	IsDir       bool   `json:"is_dir"`
}

type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
	Indexes []Index  `json:"indexes,omitempty"`
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
}

type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

type APIEndpoint struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Description string            `json:"description"`
	Auth        bool              `json:"auth"`
	Body        map[string]string `json:"body,omitempty"`
	Response    map[string]string `json:"response,omitempty"`
}

type AuthStrategy struct {
	Type      string   `json:"type"`
	Provider  string   `json:"provider,omitempty"`
	TokenType string   `json:"token_type,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
}

type DeployConfig struct {
	Platform      string            `json:"platform"`
	Containerized bool              `json:"containerized"`
	EnvVars       []string          `json:"env_vars,omitempty"`
	Ports         []int             `json:"ports,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// Task represents an implementation task created by the Planner
type Task struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Dependencies []string   `json:"dependencies"`
	Files        []string   `json:"files"`
	Priority     int        `json:"priority"`
	Status       TaskStatus `json:"status"`
	AgentPrompt  string     `json:"agent_prompt,omitempty"`
}

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)

// ReviewNote represents a review comment from the Reviewer agent
type ReviewNote struct {
	File       string       `json:"file"`
	Line       int          `json:"line,omitempty"`
	Severity   NoteSeverity `json:"severity"`
	Category   string       `json:"category"`
	Message    string       `json:"message"`
	Suggestion string       `json:"suggestion,omitempty"`
}

type NoteSeverity string

const (
	SeverityCritical   NoteSeverity = "critical"
	SeverityWarning    NoteSeverity = "warning"
	SeveritySuggestion NoteSeverity = "suggestion"
)

// TestResult represents results from the Tester agent
type TestResult struct {
	File     string  `json:"file"`
	TestName string  `json:"test_name"`
	Passed   bool    `json:"passed"`
	Coverage float64 `json:"coverage,omitempty"`
	Error    string  `json:"error,omitempty"`
	Duration string  `json:"duration,omitempty"`
}

// IterationRecord tracks a single round of user feedback
type IterationRecord struct {
	Round         int       `json:"round"`
	Feedback      string    `json:"feedback"`
	AffectedFiles []string  `json:"affected_files"`
	ChangedFiles  []string  `json:"changed_files"`
	Timestamp     time.Time `json:"timestamp"`
}

// ThoughtStep represents a single step in chain-of-thought reasoning
type ThoughtStep struct {
	Agent   AgentType `json:"agent"`
	Step    int       `json:"step"`
	Thought string    `json:"thought"`
	Action  string    `json:"action,omitempty"`
}

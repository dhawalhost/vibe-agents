package agents_test

import (
	"context"
	"testing"

	"github.com/dhawalhost/vibe-agents/internal/agents"
	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// mockProvider for testing
type mockProvider struct {
	response string
}

func (m *mockProvider) Name() string     { return "mock" }
func (m *mockProvider) Models() []string { return []string{"mock-model"} }
func (m *mockProvider) Generate(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return &llm.LLMResponse{Content: m.response, Model: "mock-model", Provider: "mock"}, nil
}
func (m *mockProvider) GenerateStream(_ context.Context, req llm.LLMRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{Content: m.response, Done: true}
	}()
	return ch, nil
}

type sequenceProvider struct {
	responses []string
	idx       int
}

func (s *sequenceProvider) Name() string     { return "mock" }
func (s *sequenceProvider) Models() []string { return []string{"mock-model"} }
func (s *sequenceProvider) Generate(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	if len(s.responses) == 0 {
		return &llm.LLMResponse{Content: "", Model: "mock-model", Provider: "mock"}, nil
	}
	if s.idx >= len(s.responses) {
		last := s.responses[len(s.responses)-1]
		return &llm.LLMResponse{Content: last, Model: "mock-model", Provider: "mock"}, nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return &llm.LLMResponse{Content: resp, Model: "mock-model", Provider: "mock"}, nil
}
func (s *sequenceProvider) GenerateStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{Done: true}
	}()
	return ch, nil
}

func TestBaseAgentName(t *testing.T) {
	mock := &mockProvider{}
	base := agents.NewBaseAgent(types.AgentArchitect, types.ReasoningCoT, mock, "mock-model")
	if base.Name() != types.AgentArchitect {
		t.Errorf("expected architect, got %s", base.Name())
	}
}

func TestBaseAgentLogThoughts(t *testing.T) {
	mock := &mockProvider{}
	base := agents.NewBaseAgent(types.AgentPlanner, types.ReasoningCoT, mock, "mock-model")
	ctx := sharedctx.New("test")

	thoughts := []string{"step 1", "step 2", "step 3"}
	base.LogThoughts(ctx, thoughts)

	cot := ctx.GetChainOfThought()
	if len(cot) != 3 {
		t.Errorf("expected 3 thoughts, got %d", len(cot))
	}
	if cot[0].Agent != types.AgentPlanner {
		t.Errorf("expected planner agent")
	}
	if cot[0].Step != 1 {
		t.Errorf("expected step 1, got %d", cot[0].Step)
	}
}

func TestBuilderParseGeneratedFiles(t *testing.T) {
	response := `Some text before

=== FILE: src/main.go ===
package main

func main() {}
=== END FILE ===

=== FILE: src/utils.go ===
package main

func helper() {}
=== END FILE ===`

	mock := &mockProvider{response: response}
	builder := agents.NewBuilderAgent(mock, "mock-model")
	ctx := sharedctx.New("test")

	tasks := []*types.Task{
		{
			ID:     "task-1",
			Title:  "Create main files",
			Files:  []string{"src/main.go", "src/utils.go"},
			Status: types.TaskPending,
		},
	}
	ctx.SetTaskGraph(tasks)
	ctx.SetBlueprint(&types.Blueprint{})

	err := builder.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, ok := ctx.GetFile("src/main.go")
	if !ok {
		t.Error("expected src/main.go to be set")
	}
	if content == "" {
		t.Error("expected non-empty content for src/main.go")
	}
}

func TestArchitectActWithValidJSON(t *testing.T) {
	blueprintJSON := `{
		"tech_stack": {"language": "Go", "framework": "Gin", "database": "PostgreSQL", "deployment": "Docker"},
		"folder_structure": [],
		"database_schema": [],
		"api_endpoints": [],
		"auth_strategy": {"type": "JWT"},
		"deployment_config": {"platform": "Docker", "containerized": true}
	}`
	mock := &mockProvider{response: blueprintJSON}
	architect := agents.NewArchitectAgent(mock, "mock-model")
	ctx := sharedctx.New("Build a REST API")

	err := architect.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bp := ctx.GetBlueprint()
	if bp == nil {
		t.Fatal("expected blueprint to be set")
	}
	if bp.TechStack.Language != "Go" {
		t.Errorf("expected Go, got %s", bp.TechStack.Language)
	}
}

func TestArchitectActRepairsNonJSONResponse(t *testing.T) {
	provider := &sequenceProvider{responses: []string{
		"Here is the high-level architecture in prose before I format it.",
		`{
			"tech_stack": {"language": "Go", "framework": "Gin", "database": "PostgreSQL", "deployment": "Docker"},
			"folder_structure": [],
			"database_schema": [],
			"api_endpoints": [],
			"auth_strategy": {"type": "JWT"},
			"deployment_config": {"platform": "Docker", "containerized": true}
		}`,
	}}
	architect := agents.NewArchitectAgent(provider, "mock-model")
	ctx := sharedctx.New("Build a REST API")

	err := architect.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bp := ctx.GetBlueprint()
	if bp == nil {
		t.Fatal("expected blueprint to be set after repair flow")
	}
	if bp.TechStack.Language != "Go" {
		t.Errorf("expected Go, got %s", bp.TechStack.Language)
	}
	if provider.idx < 2 {
		t.Errorf("expected architect to call provider twice, got %d calls", provider.idx)
	}
}

func TestPlannerActWithValidJSON(t *testing.T) {
	tasksJSON := `[
		{"id": "task-1", "title": "Setup", "description": "Setup project", "dependencies": [], "files": ["main.go"], "priority": 1, "status": "pending"},
		{"id": "task-2", "title": "API", "description": "Build API", "dependencies": ["task-1"], "files": ["api.go"], "priority": 2, "status": "pending"}
	]`
	mock := &mockProvider{response: tasksJSON}
	planner := agents.NewPlannerAgent(mock, "mock-model")
	ctx := sharedctx.New("Build a REST API")
	ctx.SetBlueprint(&types.Blueprint{})

	err := planner.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := ctx.GetTaskGraph()
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestPlannerActRepairsNonJSONResponse(t *testing.T) {
	provider := &sequenceProvider{responses: []string{
		"I will provide tasks, but first here is a summary in plain text.",
		`[
			{"id": "task-1", "title": "Setup", "description": "Setup project", "dependencies": [], "files": ["main.go"], "priority": 1, "status": "pending"}
		]`,
	}}
	planner := agents.NewPlannerAgent(provider, "mock-model")
	ctx := sharedctx.New("Build a REST API")
	ctx.SetBlueprint(&types.Blueprint{})

	err := planner.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := ctx.GetTaskGraph()
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after repair flow, got %d", len(tasks))
	}
	if provider.idx < 2 {
		t.Errorf("expected planner to call provider twice, got %d calls", provider.idx)
	}
}

func TestPlannerActRepairsTruncatedJSONLocally(t *testing.T) {
	provider := &sequenceProvider{responses: []string{
		`[
			{"id": "task-1", "title": "Setup", "description": "Setup project", "dependencies": [], "files": ["main.go"], "priority": 1, "status": "pending"}
		`,
	}}
	planner := agents.NewPlannerAgent(provider, "mock-model")
	ctx := sharedctx.New("Build a REST API")
	ctx.SetBlueprint(&types.Blueprint{})

	err := planner.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := ctx.GetTaskGraph()
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after local JSON repair, got %d", len(tasks))
	}
	if provider.idx != 1 {
		t.Errorf("expected planner to recover locally without extra repair call, got %d calls", provider.idx)
	}
}

func TestReviewerActWithValidJSON(t *testing.T) {
	notesJSON := `[
		{"file": "main.go", "severity": "warning", "category": "style", "message": "missing comment"}
	]`
	mock := &mockProvider{response: notesJSON}
	reviewer := agents.NewReviewerAgent(mock, "mock-model")
	ctx := sharedctx.New("test")
	ctx.SetFile("main.go", "package main")

	err := reviewer.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notes := ctx.GetReviewNotes()
	if len(notes) != 1 {
		t.Errorf("expected 1 review note, got %d", len(notes))
	}
}

func TestReviewerAct_DowngradesSpeculativeCriticalWithoutLine(t *testing.T) {
	notesJSON := `[
		{"file": "src/services/subscriptionService.js", "severity": "critical", "category": "security", "message": "Potential for SQL injection if user input is not sanitized."}
	]`
	mock := &mockProvider{response: notesJSON}
	reviewer := agents.NewReviewerAgent(mock, "mock-model")
	ctx := sharedctx.New("test")
	ctx.SetFile("src/services/subscriptionService.js", "const sql = 'select 1';")

	err := reviewer.Act(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notes := ctx.GetReviewNotes()
	if len(notes) != 1 {
		t.Fatalf("expected 1 review note, got %d", len(notes))
	}
	if notes[0].Severity != types.SeverityWarning {
		t.Fatalf("expected speculative critical note to be downgraded to warning, got %q", notes[0].Severity)
	}
}

func TestOrchestratorRun_ReviewerDoesNotBlockPipeline(t *testing.T) {
	provider := &sequenceProvider{responses: []string{
		`{
			"tech_stack": {"language": "Go", "framework": "Gin", "database": "PostgreSQL", "deployment": "Docker"},
			"folder_structure": [],
			"database_schema": [],
			"api_endpoints": [],
			"auth_strategy": {"type": "JWT"},
			"deployment_config": {"platform": "Docker", "containerized": true}
		}`,
		`[
			{"id": "task-1", "title": "Setup", "description": "Setup project", "dependencies": [], "files": ["main.go"], "priority": 1, "status": "pending"}
		]`,
		"=== FILE: main.go ===\npackage main\n\nfunc main() {}\n=== END FILE ===",
		`[
			{"file": "main.go", "line": 12, "severity": "critical", "category": "security", "message": "Hardcoded secret found.", "suggestion": "Move it to env vars."}
		]`,
		"=== FILE: main_test.go ===\npackage main\n\nfunc TestMain(t *testing.T) {}\n=== END FILE ===",
	}}

	architect := agents.NewArchitectAgent(provider, "mock-model")
	planner := agents.NewPlannerAgent(provider, "mock-model")
	builder := agents.NewBuilderAgent(provider, "mock-model")
	reviewer := agents.NewReviewerAgent(provider, "mock-model")
	tester := agents.NewTesterAgent(provider, "mock-model")
	iterator := agents.NewIteratorAgent(provider, "mock-model")
	orchestrator := agents.NewOrchestratorAgent(provider, "mock-model", architect, planner, builder, reviewer, tester, iterator)

	ctx := sharedctx.New("Build a tiny app")
	err := orchestrator.Run(context.Background(), ctx)
	if err != nil {
		t.Fatalf("expected reviewer to be advisory-only, got error: %v", err)
	}

	if _, ok := ctx.GetFile("main_test.go"); !ok {
		t.Fatal("expected tester to run and generate test file even after reviewer critical notes")
	}
}

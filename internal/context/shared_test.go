package context_test

import (
	"sync"
	"testing"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

func TestNew(t *testing.T) {
	ctx := sharedctx.New("test prompt")
	if ctx.UserPrompt != "test prompt" {
		t.Errorf("expected prompt %q, got %q", "test prompt", ctx.UserPrompt)
	}
	if ctx.GeneratedFiles == nil {
		t.Error("expected GeneratedFiles to be initialized")
	}
}

func TestSetGetBlueprint(t *testing.T) {
	ctx := sharedctx.New("test")
	bp := &types.Blueprint{
		TechStack: types.TechStack{Language: "Go", Framework: "Gin"},
	}
	ctx.SetBlueprint(bp)
	got := ctx.GetBlueprint()
	if got == nil {
		t.Fatal("expected blueprint, got nil")
	}
	if got.TechStack.Language != "Go" {
		t.Errorf("expected language Go, got %s", got.TechStack.Language)
	}
}

func TestSetGetFile(t *testing.T) {
	ctx := sharedctx.New("test")
	ctx.SetFile("main.go", "package main")

	content, ok := ctx.GetFile("main.go")
	if !ok {
		t.Error("expected file to exist")
	}
	if content != "package main" {
		t.Errorf("expected %q, got %q", "package main", content)
	}

	_, ok = ctx.GetFile("nonexistent.go")
	if ok {
		t.Error("expected file to not exist")
	}
}

func TestGetAllFiles(t *testing.T) {
	ctx := sharedctx.New("test")
	ctx.SetFile("a.go", "content a")
	ctx.SetFile("b.go", "content b")

	files := ctx.GetAllFiles()
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
	if files["a.go"] != "content a" {
		t.Errorf("expected content a, got %q", files["a.go"])
	}
}

func TestAddGetReviewNotes(t *testing.T) {
	ctx := sharedctx.New("test")
	note := &types.ReviewNote{
		File:     "main.go",
		Severity: types.SeverityWarning,
		Message:  "test warning",
	}
	ctx.AddReviewNote(note)

	notes := ctx.GetReviewNotes()
	if len(notes) != 1 {
		t.Errorf("expected 1 note, got %d", len(notes))
	}
}

func TestHasCriticalIssues(t *testing.T) {
	ctx := sharedctx.New("test")
	if ctx.HasCriticalIssues() {
		t.Error("expected no critical issues")
	}

	ctx.AddReviewNote(&types.ReviewNote{Severity: types.SeverityWarning})
	if ctx.HasCriticalIssues() {
		t.Error("expected no critical issues with only warnings")
	}

	ctx.AddReviewNote(&types.ReviewNote{Severity: types.SeverityCritical, File: "auth.js"})
	if ctx.HasCriticalIssues() {
		t.Error("expected non-evidenced critical note to not block pipeline")
	}

	ctx.AddReviewNote(&types.ReviewNote{Severity: types.SeverityCritical, File: "auth.js", Line: 42})
	if !ctx.HasCriticalIssues() {
		t.Error("expected evidenced critical issues to block pipeline")
	}
}

func TestClearReviewNotes(t *testing.T) {
	ctx := sharedctx.New("test")
	ctx.AddReviewNote(&types.ReviewNote{Severity: types.SeverityCritical})
	ctx.ClearReviewNotes()
	if len(ctx.GetReviewNotes()) != 0 {
		t.Error("expected review notes to be cleared")
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	ctx := sharedctx.New("test")
	tasks := []*types.Task{
		{ID: "task-1", Status: types.TaskPending},
		{ID: "task-2", Status: types.TaskPending},
	}
	ctx.SetTaskGraph(tasks)

	err := ctx.UpdateTaskStatus("task-1", types.TaskCompleted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	graph := ctx.GetTaskGraph()
	if graph[0].Status != types.TaskCompleted {
		t.Errorf("expected task-1 to be completed")
	}

	err = ctx.UpdateTaskStatus("nonexistent", types.TaskCompleted)
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestGetPendingTasks(t *testing.T) {
	ctx := sharedctx.New("test")
	tasks := []*types.Task{
		{ID: "task-1", Status: types.TaskPending, Dependencies: []string{}},
		{ID: "task-2", Status: types.TaskPending, Dependencies: []string{"task-1"}},
		{ID: "task-3", Status: types.TaskCompleted, Dependencies: []string{}},
	}
	ctx.SetTaskGraph(tasks)

	pending := ctx.GetPendingTasks()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending task (task-1), got %d", len(pending))
	}
	if pending[0].ID != "task-1" {
		t.Errorf("expected task-1, got %s", pending[0].ID)
	}
}

func TestThreadSafety(t *testing.T) {
	ctx := sharedctx.New("test")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx.SetFile(string(rune('a'+i%26)), "content")
			ctx.GetAllFiles()
			ctx.AddReviewNote(&types.ReviewNote{Severity: types.SeverityWarning})
			ctx.GetReviewNotes()
		}(i)
	}
	wg.Wait()
}

func TestSerialize(t *testing.T) {
	ctx := sharedctx.New("test prompt")
	ctx.SetFile("main.go", "package main")

	data, err := ctx.Serialize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty serialized data")
	}
}

func TestAddThought(t *testing.T) {
	ctx := sharedctx.New("test")
	ctx.AddThought(types.AgentArchitect, 1, "thinking...", "design")

	thoughts := ctx.GetChainOfThought()
	if len(thoughts) != 1 {
		t.Errorf("expected 1 thought, got %d", len(thoughts))
	}
	if thoughts[0].Agent != types.AgentArchitect {
		t.Errorf("expected architect agent")
	}
}

func TestGetLatestFeedback(t *testing.T) {
	ctx := sharedctx.New("test")
	if ctx.GetLatestFeedback() != "" {
		t.Error("expected empty feedback")
	}

	ctx.AddIterationRecord(&types.IterationRecord{Round: 1, Feedback: "add dark mode"})
	ctx.AddIterationRecord(&types.IterationRecord{Round: 2, Feedback: "fix bugs"})

	if ctx.GetLatestFeedback() != "fix bugs" {
		t.Errorf("expected %q, got %q", "fix bugs", ctx.GetLatestFeedback())
	}
}

package prompt_test

import (
	"strings"
	"testing"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

func TestBuildArchitectPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build a REST API with auth")

	p := b.BuildArchitectPrompt(ctx)

	if !strings.Contains(p, "Build a REST API with auth") {
		t.Error("expected prompt to contain user request")
	}
	if !strings.Contains(p, "tech_stack") {
		t.Error("expected prompt to contain tech_stack schema")
	}
	if !strings.Contains(p, "JSON") {
		t.Error("expected prompt to mention JSON")
	}
}

func TestBuildPlannerPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build a REST API")
	ctx.SetBlueprint(&types.Blueprint{
		TechStack: types.TechStack{Language: "Go", Framework: "Gin"},
	})

	p := b.BuildPlannerPrompt(ctx)

	if !strings.Contains(p, "Build a REST API") {
		t.Error("expected prompt to contain user request")
	}
	if !strings.Contains(p, "Go") {
		t.Error("expected prompt to include blueprint tech stack")
	}
}

func TestBuildBuilderPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build an API")
	ctx.SetBlueprint(&types.Blueprint{
		TechStack: types.TechStack{Language: "Go"},
	})

	task := &types.Task{
		ID:          "task-1",
		Title:       "Create main.go",
		Description: "Initialize the Go project",
		Files:       []string{"main.go"},
		AgentPrompt: "Create the entry point",
	}

	p := b.BuildBuilderPrompt(ctx, task)

	if !strings.Contains(p, "Build an API") {
		t.Error("expected prompt to contain user request")
	}
	if !strings.Contains(p, "Create main.go") {
		t.Error("expected prompt to contain task title")
	}
	if !strings.Contains(p, "main.go") {
		t.Error("expected prompt to contain file name")
	}
	if !strings.Contains(p, "=== FILE:") {
		t.Error("expected prompt to contain file format instructions")
	}
}

func TestBuildReviewerPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build an API")
	ctx.SetFile("main.go", "package main\nfunc main() {}")

	p := b.BuildReviewerPrompt(ctx)

	if !strings.Contains(p, "main.go") {
		t.Error("expected prompt to contain file name")
	}
	if !strings.Contains(p, "severity") {
		t.Error("expected prompt to mention severity levels")
	}
}

func TestBuildTesterPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build an API")
	ctx.SetBlueprint(&types.Blueprint{
		TechStack: types.TechStack{Language: "Go", Framework: "Gin"},
	})
	ctx.SetFile("main.go", "package main")

	p := b.BuildTesterPrompt(ctx)

	if !strings.Contains(p, "Go") {
		t.Error("expected prompt to mention language")
	}
	if !strings.Contains(p, "Gin") {
		t.Error("expected prompt to mention framework")
	}
	if !strings.Contains(p, "=== FILE:") {
		t.Error("expected prompt to contain file format instructions")
	}
}

func TestBuildIteratorPrompt(t *testing.T) {
	b := prompt.New()
	ctx := sharedctx.New("Build an API")
	ctx.SetFile("main.go", "package main")
	ctx.AddIterationRecord(&types.IterationRecord{
		Round:    1,
		Feedback: "add rate limiting",
	})

	p := b.BuildIteratorPrompt(ctx)

	if !strings.Contains(p, "add rate limiting") {
		t.Error("expected prompt to contain user feedback")
	}
	if !strings.Contains(strings.ToLower(p), "minimal") {
		t.Error("expected prompt to mention minimal changes")
	}
}

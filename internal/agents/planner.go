package agents

import (
	"context"
	"encoding/json"
	"fmt"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// PlannerAgent creates an ordered task graph from the blueprint
type PlannerAgent struct {
	*BaseAgent
}

func NewPlannerAgent(provider llm.LLMProvider, model string) *PlannerAgent {
	return &PlannerAgent{
		BaseAgent: NewBaseAgent(types.AgentPlanner, types.ReasoningCoT, provider, model),
	}
}

func (p *PlannerAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	thoughts := []string{
		"Analyzing system blueprint for implementation tasks",
		"Identifying dependencies between components",
		"Ordering tasks topologically (foundation → services → APIs → UI)",
		"Generating specific builder prompts for each task",
		"Validating task dependency graph for cycles",
	}
	p.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (p *PlannerAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := p.Think(ctx, sharedCtx); err != nil {
		return err
	}

	pb := prompt.New()
	userPrompt := pb.BuildPlannerPrompt(sharedCtx)

	response, err := p.GenerateWithSystem(ctx, prompt.SystemPromptPlanner, userPrompt)
	if err != nil {
		return fmt.Errorf("planner LLM call failed: %w", err)
	}

	tasks, err := parseTasks(response)
	if err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}

	sharedCtx.SetTaskGraph(tasks)
	return nil
}

func parseTasks(response string) ([]*types.Task, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in planner response")
	}

	var tasks []*types.Task
	if err := json.Unmarshal([]byte(jsonStr), &tasks); err != nil {
		return nil, fmt.Errorf("unmarshal tasks: %w", err)
	}

	// Set all tasks to pending status
	for _, t := range tasks {
		if t.Status == "" {
			t.Status = types.TaskPending
		}
	}

	return tasks, nil
}

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
		initialParseErr := err
		repairPrompt := fmt.Sprintf(
			"Your previous response was not valid task JSON. Convert it to valid JSON now.\n\n"+
				"Return ONLY a JSON array of tasks following this schema:\n%s\n\n"+
				"Previous response to repair:\n%s",
			prompt.TaskJSONSchema,
			strings.TrimSpace(response),
		)

		repaired, repairErr := p.GenerateWithSystem(ctx, prompt.SystemPromptPlanner, repairPrompt)
		if repairErr != nil {
			return fmt.Errorf("parse tasks: %w (repair generation failed: %v)", err, repairErr)
		}

		tasks, err = parseTasks(repaired)
		if err != nil {
			return fmt.Errorf("parse tasks: %v (repair parse failed: %w)", initialParseErr, err)
		}
	}

	sharedCtx.SetTaskGraph(tasks)
	return nil
}

func parseTasks(response string) ([]*types.Task, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in planner response")
	}
	jsonStr = normalizeJSONCandidate(jsonStr)

	var tasks []*types.Task
	if err := json.Unmarshal([]byte(jsonStr), &tasks); err != nil {
		// Some models wrap the task list in an object instead of returning a raw array.
		var wrapped struct {
			Tasks     []*types.Task `json:"tasks"`
			TaskGraph []*types.Task `json:"task_graph"`
			Data      []*types.Task `json:"data"`
		}
		if err2 := json.Unmarshal([]byte(jsonStr), &wrapped); err2 != nil {
			return nil, fmt.Errorf("unmarshal tasks: %w", err)
		}
		switch {
		case len(wrapped.Tasks) > 0:
			tasks = wrapped.Tasks
		case len(wrapped.TaskGraph) > 0:
			tasks = wrapped.TaskGraph
		case len(wrapped.Data) > 0:
			tasks = wrapped.Data
		default:
			return nil, fmt.Errorf("unmarshal tasks: JSON did not contain a task array")
		}
	}

	// Set all tasks to pending status
	for _, t := range tasks {
		if t.Status == "" {
			t.Status = types.TaskPending
		}
	}

	return tasks, nil
}

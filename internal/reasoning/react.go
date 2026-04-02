package reasoning

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// ReActStep represents one iteration of the ReAct loop
type ReActStep struct {
	Thought string
	Action  string
	Result  string
}

// ActionFunc is a function that executes an action and returns a result
type ActionFunc func(ctx context.Context, action string) (string, error)

// ReAct implements the Reason + Act reasoning loop
type ReAct struct {
	provider llm.LLMProvider
	model    string
	maxSteps int
	actions  map[string]ActionFunc
}

func NewReAct(provider llm.LLMProvider, model string, maxSteps int) *ReAct {
	return &ReAct{
		provider: provider,
		model:    model,
		maxSteps: maxSteps,
		actions:  make(map[string]ActionFunc),
	}
}

func (r *ReAct) RegisterAction(name string, fn ActionFunc) {
	r.actions[name] = fn
}

// Reason performs the ReAct loop for complex decision making
func (r *ReAct) Reason(ctx context.Context, agentType types.AgentType, goal string) ([]string, []*ReActStep, error) {
	var steps []*ReActStep
	var thoughts []string

	history := fmt.Sprintf("Goal: %s\n\nAvailable actions: %s\n\n", goal, r.getActionNames())

	for i := 0; i < r.maxSteps; i++ {
		prompt := fmt.Sprintf(`You are a %s agent using the ReAct framework (Reason + Act).

%s
Step %d:
Think about what to do next (Thought), then decide on an action.

Format your response as:
Thought: [your reasoning about the current state and what to do]
Action: [action_name: action_details]

Available actions: %s
Or say "Finish: [conclusion]" if the goal is achieved.`,
			agentType, history, i+1, r.getActionNames())

		req := llm.LLMRequest{
			Model: r.model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: 0.3,
		}

		resp, err := r.provider.Generate(ctx, req)
		if err != nil {
			return thoughts, steps, fmt.Errorf("ReAct step %d failed: %w", i, err)
		}

		thought, action := parseReActResponse(resp.Content)
		thoughts = append(thoughts, fmt.Sprintf("Step %d - Thought: %s", i+1, thought))

		if strings.HasPrefix(action, "Finish:") {
			conclusion := strings.TrimPrefix(action, "Finish:")
			thoughts = append(thoughts, fmt.Sprintf("Conclusion: %s", strings.TrimSpace(conclusion)))
			break
		}

		// Execute action
		result := "Action executed"
		actionName, actionDetails := splitAction(action)
		if fn, ok := r.actions[actionName]; ok {
			if res, err := fn(ctx, actionDetails); err == nil {
				result = res
			}
		}

		step := &ReActStep{
			Thought: thought,
			Action:  action,
			Result:  result,
		}
		steps = append(steps, step)
		history += fmt.Sprintf("Thought: %s\nAction: %s\nResult: %s\n\n", thought, action, result)
	}

	return thoughts, steps, nil
}

func (r *ReAct) getActionNames() string {
	names := make([]string, 0, len(r.actions))
	for name := range r.actions {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func parseReActResponse(content string) (thought, action string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Thought:") {
			thought = strings.TrimSpace(strings.TrimPrefix(line, "Thought:"))
		} else if strings.HasPrefix(line, "Action:") || strings.HasPrefix(line, "Finish:") {
			action = strings.TrimSpace(line)
		}
	}
	return
}

func splitAction(action string) (name, details string) {
	// Remove "Action:" prefix if present
	action = strings.TrimPrefix(action, "Action:")
	action = strings.TrimSpace(action)

	parts := strings.SplitN(action, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return action, ""
}

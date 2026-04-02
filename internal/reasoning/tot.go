package reasoning

import (
	"context"
	"fmt"

	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// ThoughtNode represents a node in the thought tree
type ThoughtNode struct {
	Thought  string
	Score    float64
	Children []*ThoughtNode
}

// TreeOfThought implements branching reasoning with path selection
type TreeOfThought struct {
	provider llm.LLMProvider
	model    string
	branches int
	maxDepth int
}

func NewTreeOfThought(provider llm.LLMProvider, model string, branches, maxDepth int) *TreeOfThought {
	return &TreeOfThought{
		provider: provider,
		model:    model,
		branches: branches,
		maxDepth: maxDepth,
	}
}

// Reason explores multiple reasoning paths and returns the best one
func (t *TreeOfThought) Reason(ctx context.Context, agentType types.AgentType, problem string) ([]string, error) {
	// Generate multiple initial thoughts
	prompt := fmt.Sprintf(`You are a %s agent performing deep analysis.

Problem: %s

Generate %d different analytical approaches to solve this problem. For each approach:
1. State the approach
2. Explain why it might work
3. Identify potential issues

Format as:
APPROACH 1: [title]
[detailed reasoning]
SCORE: [0-10 confidence]

APPROACH 2: [title]
...`, agentType, problem, t.branches)

	req := llm.LLMRequest{
		Model: t.model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7, // Higher temperature for diverse thoughts
	}

	resp, err := t.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ToT reasoning failed: %w", err)
	}

	// Now select the best approach
	selectPrompt := fmt.Sprintf(`Given these approaches to solving:
"%s"

The proposed approaches were:
%s

Which approach is best? Synthesize the best elements into a final recommendation.
Provide your final, consolidated reasoning as numbered steps.`, problem, resp.Content)

	selectReq := llm.LLMRequest{
		Model: t.model,
		Messages: []llm.Message{
			{Role: "user", Content: selectPrompt},
		},
		Temperature: 0.2,
	}

	finalResp, err := t.provider.Generate(ctx, selectReq)
	if err != nil {
		return nil, fmt.Errorf("ToT selection failed: %w", err)
	}

	return parseSteps(finalResp.Content), nil
}

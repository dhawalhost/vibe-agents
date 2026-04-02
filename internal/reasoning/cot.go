package reasoning

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// ChainOfThought implements linear step-by-step reasoning
type ChainOfThought struct {
	provider llm.LLMProvider
	model    string
}

func NewChainOfThought(provider llm.LLMProvider, model string) *ChainOfThought {
	return &ChainOfThought{provider: provider, model: model}
}

// Reason generates reasoning steps for a given problem using CoT prompting
func (c *ChainOfThought) Reason(ctx context.Context, agentType types.AgentType, problem string) ([]string, error) {
	prompt := fmt.Sprintf(`You are a %s agent. Think through this step by step.

Problem: %s

Think through this carefully, one step at a time. For each step, explain your reasoning clearly.
Format your response as numbered steps:
1. [First reasoning step]
2. [Second reasoning step]
...

Be thorough and systematic in your analysis.`, agentType, problem)

	req := llm.LLMRequest{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
	}

	resp, err := c.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CoT reasoning failed: %w", err)
	}

	return parseSteps(resp.Content), nil
}

// ReasonWithContext reasons about a problem given additional context
func (c *ChainOfThought) ReasonWithContext(ctx context.Context, agentType types.AgentType, problem string, additionalContext string) ([]string, error) {
	prompt := fmt.Sprintf(`You are a %s agent. Think through this step by step.

Context:
%s

Problem: %s

Think through this carefully, one step at a time.
Format your response as numbered steps:
1. [First reasoning step]
2. [Second reasoning step]
...`, agentType, additionalContext, problem)

	req := llm.LLMRequest{
		Model: c.model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
	}

	resp, err := c.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CoT reasoning failed: %w", err)
	}

	return parseSteps(resp.Content), nil
}

func parseSteps(content string) []string {
	lines := strings.Split(content, "\n")
	var steps []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		steps = append(steps, line)
	}
	return steps
}

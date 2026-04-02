package agents

import (
	"context"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// Agent defines the interface that all agents must implement
type Agent interface {
	// Name returns the agent's identifier
	Name() types.AgentType
	// Think produces reasoning steps before acting (Chain-of-Thought)
	Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error)
	// Act performs the agent's primary action and updates the shared context
	Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error
}

// BaseAgent provides common functionality for all agents
type BaseAgent struct {
	agentType types.AgentType
	strategy  types.ReasoningStrategy
	provider  llm.LLMProvider
	model     string
	prompt    *prompt.Builder
}

func NewBaseAgent(
	agentType types.AgentType,
	strategy types.ReasoningStrategy,
	provider llm.LLMProvider,
	model string,
) *BaseAgent {
	return &BaseAgent{
		agentType: agentType,
		strategy:  strategy,
		provider:  provider,
		model:     model,
		prompt:    prompt.New(),
	}
}

func (b *BaseAgent) Name() types.AgentType {
	return b.agentType
}

func (b *BaseAgent) Strategy() types.ReasoningStrategy {
	return b.strategy
}

func (b *BaseAgent) Provider() llm.LLMProvider {
	return b.provider
}

func (b *BaseAgent) Model() string {
	return b.model
}

func (b *BaseAgent) PromptBuilder() *prompt.Builder {
	return b.prompt
}

// LogThoughts records thinking steps to the shared context
func (b *BaseAgent) LogThoughts(sharedCtx *sharedctx.SharedContext, thoughts []string) {
	for i, thought := range thoughts {
		sharedCtx.AddThought(b.agentType, i+1, thought, "")
	}
}

// Generate calls the LLM provider
func (b *BaseAgent) Generate(ctx context.Context, userPrompt string) (string, error) {
	req := llm.LLMRequest{
		Model: b.model,
		Messages: []llm.Message{
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   8192,
		Temperature: 0.3,
	}
	resp, err := b.provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// GenerateWithSystem calls the LLM provider with a system prompt
func (b *BaseAgent) GenerateWithSystem(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	req := llm.LLMRequest{
		Model:        b.model,
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   8192,
		Temperature: 0.3,
	}
	resp, err := b.provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

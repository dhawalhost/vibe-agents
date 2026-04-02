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

// ArchitectAgent designs system architecture
type ArchitectAgent struct {
	*BaseAgent
}

func NewArchitectAgent(provider llm.LLMProvider, model string) *ArchitectAgent {
	return &ArchitectAgent{
		BaseAgent: NewBaseAgent(types.AgentArchitect, types.ReasoningCoT, provider, model),
	}
}

func (a *ArchitectAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	thoughts := []string{
		fmt.Sprintf("Analyzing user request: %q", sharedCtx.UserPrompt),
		"Identifying core features and functional requirements",
		"Evaluating tech stack options for the requirements",
		"Designing data model and persistence strategy",
		"Planning API surface and authentication approach",
		"Determining deployment and infrastructure requirements",
		"Composing system blueprint as structured JSON",
	}
	a.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (a *ArchitectAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := a.Think(ctx, sharedCtx); err != nil {
		return err
	}

	pb := prompt.New()
	userPrompt := pb.BuildArchitectPrompt(sharedCtx)

	response, err := a.GenerateWithSystem(ctx, prompt.SystemPromptArchitect, userPrompt)
	if err != nil {
		return fmt.Errorf("architect LLM call failed: %w", err)
	}

	blueprint, err := parseBlueprint(response)
	if err != nil {
		return fmt.Errorf("parse blueprint: %w", err)
	}

	sharedCtx.SetBlueprint(blueprint)
	return nil
}

func parseBlueprint(response string) (*types.Blueprint, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in architect response")
	}

	var bp types.Blueprint
	if err := json.Unmarshal([]byte(jsonStr), &bp); err != nil {
		return nil, fmt.Errorf("unmarshal blueprint: %w", err)
	}
	return &bp, nil
}

// extractJSON tries to find and extract a JSON object from text
func extractJSON(text string) string {
	// Try to find JSON between code blocks first
	if idx := strings.Index(text, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(text[start:], "```")
		if end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx != -1 {
		start := idx + 3
		end := strings.Index(text[start:], "```")
		if end != -1 {
			candidate := strings.TrimSpace(text[start : start+end])
			if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
				return candidate
			}
		}
	}

	// Try to find raw JSON - pick whichever comes first
	startBrace := strings.Index(text, "{")
	startBracket := strings.Index(text, "[")

	var start int
	var opener, closer rune
	switch {
	case startBrace == -1 && startBracket == -1:
		return ""
	case startBrace == -1:
		start, opener, closer = startBracket, '[', ']'
	case startBracket == -1:
		start, opener, closer = startBrace, '{', '}'
	case startBracket < startBrace:
		start, opener, closer = startBracket, '[', ']'
	default:
		start, opener, closer = startBrace, '{', '}'
	}

	depth := 0
	for i, ch := range text[start:] {
		if ch == opener {
			depth++
		} else if ch == closer {
			depth--
			if depth == 0 {
				return text[start : start+i+1]
			}
		}
	}
	return ""
}

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
		initialParseErr := err
		repairPrompt := fmt.Sprintf(
			"Your previous response was not valid blueprint JSON. Convert it to valid JSON now.\n\n"+
				"Return ONLY a JSON object following this schema:\n%s\n\n"+
				"Previous response to repair:\n%s",
			prompt.BlueprintJSONSchema,
			strings.TrimSpace(response),
		)

		repaired, repairErr := a.GenerateWithSystem(ctx, prompt.SystemPromptArchitect, repairPrompt)
		if repairErr != nil {
			return fmt.Errorf("parse blueprint: %w (repair generation failed: %v)", err, repairErr)
		}

		blueprint, err = parseBlueprint(repaired)
		if err != nil {
			return fmt.Errorf("parse blueprint: %v (repair parse failed: %w)", initialParseErr, err)
		}
	}

	sharedCtx.SetBlueprint(blueprint)
	return nil
}

func parseBlueprint(response string) (*types.Blueprint, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in architect response")
	}
	jsonStr = normalizeJSONCandidate(jsonStr)

	var bp types.Blueprint
	if err := json.Unmarshal([]byte(jsonStr), &bp); err != nil {
		var wrapped struct {
			Blueprint    *types.Blueprint `json:"blueprint"`
			Architecture *types.Blueprint `json:"architecture"`
			Data         *types.Blueprint `json:"data"`
		}
		if err2 := json.Unmarshal([]byte(jsonStr), &wrapped); err2 != nil {
			return nil, fmt.Errorf("unmarshal blueprint: %w", err)
		}
		switch {
		case wrapped.Blueprint != nil:
			bp = *wrapped.Blueprint
		case wrapped.Architecture != nil:
			bp = *wrapped.Architecture
		case wrapped.Data != nil:
			bp = *wrapped.Data
		default:
			return nil, fmt.Errorf("unmarshal blueprint: JSON did not contain blueprint object")
		}
	}
	return &bp, nil
}

func normalizeJSONCandidate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = stripTrailingJSONCommas(s)
	s = closeOpenJSONStructures(s)
	s = stripTrailingJSONCommas(s)
	return s
}

func stripTrailingJSONCommas(s string) string {
	var b strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(s) {
				switch s[j] {
				case ' ', '\n', '\r', '\t':
					j++
				default:
					goto trailingCheck
				}
			}
		trailingCheck:
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func closeOpenJSONStructures(s string) string {
	stack := make([]rune, 0)
	inString := false
	escaped := false
	for _, ch := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if inString {
		s += "\""
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
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

	// If the model returned truncated JSON, return the tail so local repair
	// logic can balance the remaining braces/brackets.
	if start >= 0 && start < len(text) {
		return strings.TrimSpace(text[start:])
	}
	return ""
}

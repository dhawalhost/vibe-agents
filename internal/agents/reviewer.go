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

// ReviewerAgent reviews generated code
type ReviewerAgent struct {
	*BaseAgent
}

func NewReviewerAgent(provider llm.LLMProvider, model string) *ReviewerAgent {
	return &ReviewerAgent{
		BaseAgent: NewBaseAgent(types.AgentReviewer, types.ReasoningToT, provider, model),
	}
}

func (r *ReviewerAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	files := sharedCtx.GetAllFiles()
	thoughts := []string{
		fmt.Sprintf("Reviewing %d generated files", len(files)),
		"Checking for security vulnerabilities",
		"Validating error handling and edge cases",
		"Verifying consistency with architectural blueprint",
		"Assessing code quality and best practices",
		"Identifying critical issues that block deployment",
	}
	r.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (r *ReviewerAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := r.Think(ctx, sharedCtx); err != nil {
		return err
	}

	pb := prompt.New()
	userPrompt := pb.BuildReviewerPrompt(sharedCtx)

	response, err := r.GenerateWithSystem(ctx, prompt.SystemPromptReviewer, userPrompt)
	if err != nil {
		return fmt.Errorf("reviewer LLM call failed: %w", err)
	}

	notes, err := parseReviewNotes(response)
	if err != nil {
		// Non-fatal - just log the error and continue with empty notes
		fmt.Printf("Warning: could not parse review notes: %v\n", err)
		return nil
	}

	for _, note := range notes {
		sharedCtx.AddReviewNote(note)
	}

	// Print summary
	if len(notes) > 0 {
		fmt.Printf("  Found %d review notes\n", len(notes))
		for _, note := range notes {
			if note.Severity == types.SeverityCritical {
				fmt.Printf("  ❌ CRITICAL [%s] %s: %s\n", note.File, note.Category, note.Message)
			}
		}
	} else {
		fmt.Println("  ✅ No issues found")
	}

	return nil
}

func parseReviewNotes(response string) ([]*types.ReviewNote, error) {
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in reviewer response")
	}

	var notes []*types.ReviewNote
	if err := json.Unmarshal([]byte(jsonStr), &notes); err != nil {
		return nil, fmt.Errorf("unmarshal review notes: %w", err)
	}
	return notes, nil
}

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// IteratorAgent handles iterative refinements based on user feedback
type IteratorAgent struct {
	*BaseAgent
}

func NewIteratorAgent(provider llm.LLMProvider, model string) *IteratorAgent {
	return &IteratorAgent{
		BaseAgent: NewBaseAgent(types.AgentIterator, types.ReasoningReAct, provider, model),
	}
}

func (it *IteratorAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	feedback := sharedCtx.GetLatestFeedback()
	thoughts := []string{
		fmt.Sprintf("Processing user feedback: %q", feedback),
		"Analyzing which components are affected by this change",
		"Calculating minimal change set to avoid full regeneration",
		"Checking for cascading impacts to dependent files",
		"Planning targeted modifications",
	}
	it.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (it *IteratorAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := it.Think(ctx, sharedCtx); err != nil {
		return err
	}

	pb := prompt.New()
	userPrompt := pb.BuildIteratorPrompt(sharedCtx)

	response, err := it.GenerateWithSystem(ctx, prompt.SystemPromptIterator, userPrompt)
	if err != nil {
		return fmt.Errorf("iterator LLM call failed: %w", err)
	}

	// Parse the response to find affected files and changes
	affectedFiles := parseAffectedFiles(response)

	// Parse and apply changed files
	changedFiles := parseGeneratedFiles(response)
	for path, content := range changedFiles {
		sharedCtx.SetFile(path, content)
	}

	// Update iteration record with affected/changed files
	if len(sharedCtx.IterationHistory) > 0 {
		record := sharedCtx.IterationHistory[len(sharedCtx.IterationHistory)-1]
		record.AffectedFiles = affectedFiles
		record.ChangedFiles = make([]string, 0, len(changedFiles))
		for path := range changedFiles {
			record.ChangedFiles = append(record.ChangedFiles, path)
		}
		record.Timestamp = time.Now()
	}

	fmt.Printf("  Modified %d files\n", len(changedFiles))
	return nil
}

func parseAffectedFiles(response string) []string {
	// Try to find the JSON analysis section
	jsonStr := ""
	if idx := strings.Index(response, `"affected_files"`); idx != -1 {
		// Find the enclosing JSON object
		start := strings.LastIndex(response[:idx], "{")
		if start != -1 {
			end := strings.Index(response[start:], "}")
			if end != -1 {
				jsonStr = response[start : start+end+1]
			}
		}
	}

	if jsonStr == "" {
		return nil
	}

	var analysis struct {
		AffectedFiles []string `json:"affected_files"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil
	}
	return analysis.AffectedFiles
}

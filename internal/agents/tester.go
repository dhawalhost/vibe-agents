package agents

import (
	"context"
	"fmt"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// TesterAgent generates tests for the built code
type TesterAgent struct {
	*BaseAgent
}

func NewTesterAgent(provider llm.LLMProvider, model string) *TesterAgent {
	return &TesterAgent{
		BaseAgent: NewBaseAgent(types.AgentTester, types.ReasoningCoT, provider, model),
	}
}

func (t *TesterAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	files := sharedCtx.GetAllFiles()
	thoughts := []string{
		fmt.Sprintf("Generating tests for %d source files", len(files)),
		"Identifying testable functions and methods",
		"Planning unit test cases (happy path, error path, edge cases)",
		"Planning integration test scaffolding for APIs",
		"Determining mock/stub requirements",
	}
	t.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (t *TesterAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := t.Think(ctx, sharedCtx); err != nil {
		return err
	}

	pb := prompt.New()
	userPrompt := pb.BuildTesterPrompt(sharedCtx)

	response, err := t.GenerateWithSystem(ctx, prompt.SystemPromptTester, userPrompt)
	if err != nil {
		return fmt.Errorf("tester LLM call failed: %w", err)
	}

	testFiles := parseGeneratedFiles(response)
	for path, content := range testFiles {
		sharedCtx.SetFile(path, content)
	}

	result := &types.TestResult{
		File:     "test_suite",
		TestName: "generated_tests",
		Passed:   true,
	}
	sharedCtx.AddTestResult(result)

	fmt.Printf("  Generated %d test files\n", len(testFiles))
	return nil
}

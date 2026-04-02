package agents

import (
	"context"
	"fmt"
	"strings"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/prompt"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// BuilderAgent generates code for each task
type BuilderAgent struct {
	*BaseAgent
}

func NewBuilderAgent(provider llm.LLMProvider, model string) *BuilderAgent {
	return &BuilderAgent{
		BaseAgent: NewBaseAgent(types.AgentBuilder, types.ReasoningCoT, provider, model),
	}
}

func (b *BuilderAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	tasks := sharedCtx.GetTaskGraph()
	thoughts := []string{
		fmt.Sprintf("Processing %d implementation tasks", len(tasks)),
		"Generating code file-by-file with full context awareness",
		"Maintaining consistency across generated files",
		"Following architectural blueprint exactly",
	}
	b.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

func (b *BuilderAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	if _, err := b.Think(ctx, sharedCtx); err != nil {
		return err
	}

	tasks := sharedCtx.GetTaskGraph()
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks to build")
	}

	// Process tasks in order (respecting dependencies)
	for _, task := range tasks {
		if task.Status == types.TaskCompleted {
			continue
		}

		fmt.Printf("  Building task: %s\n", task.Title)
		if err := b.buildTask(ctx, sharedCtx, task); err != nil {
			task.Status = types.TaskFailed
			return fmt.Errorf("build task %q: %w", task.Title, err)
		}
		task.Status = types.TaskCompleted
	}

	return nil
}

func (b *BuilderAgent) buildTask(ctx context.Context, sharedCtx *sharedctx.SharedContext, task *types.Task) error {
	task.Status = types.TaskInProgress

	pb := prompt.New()
	userPrompt := pb.BuildBuilderPrompt(sharedCtx, task)

	response, err := b.GenerateWithSystem(ctx, prompt.SystemPromptBuilder, userPrompt)
	if err != nil {
		return fmt.Errorf("builder LLM call failed: %w", err)
	}

	files := parseGeneratedFiles(response)
	if len(files) == 0 {
		// If no structured files found, save the entire response as a single file
		if len(task.Files) > 0 {
			sharedCtx.SetFile(task.Files[0], response)
		}
		return nil
	}

	for path, content := range files {
		sharedCtx.SetFile(path, content)
	}

	return nil
}

// parseGeneratedFiles parses the === FILE: ... === format from LLM responses
func parseGeneratedFiles(response string) map[string]string {
	files := make(map[string]string)
	lines := strings.Split(response, "\n")

	var currentFile string
	var contentLines []string
	inFile := false

	for _, line := range lines {
		if strings.HasPrefix(line, "=== FILE:") && strings.HasSuffix(strings.TrimSpace(line), "===") {
			// Save previous file if any
			if inFile && currentFile != "" {
				files[currentFile] = strings.TrimSpace(strings.Join(contentLines, "\n"))
			}
			// Extract file path
			trimmed := strings.TrimSpace(line)
			trimmed = strings.TrimPrefix(trimmed, "=== FILE:")
			trimmed = strings.TrimSuffix(trimmed, "===")
			currentFile = strings.TrimSpace(trimmed)
			contentLines = nil
			inFile = true
		} else if strings.TrimSpace(line) == "=== END FILE ===" {
			if inFile && currentFile != "" {
				files[currentFile] = strings.TrimSpace(strings.Join(contentLines, "\n"))
			}
			currentFile = ""
			contentLines = nil
			inFile = false
		} else if inFile {
			contentLines = append(contentLines, line)
		}
	}

	// Handle case where file wasn't closed
	if inFile && currentFile != "" {
		files[currentFile] = strings.TrimSpace(strings.Join(contentLines, "\n"))
	}

	return files
}

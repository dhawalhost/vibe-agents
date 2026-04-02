package prompt

import (
	"encoding/json"
	"fmt"
	"strings"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

// Builder constructs dynamic, context-aware prompts
type Builder struct{}

func New() *Builder {
	return &Builder{}
}

// BuildArchitectPrompt constructs a prompt for the Architect agent
func (b *Builder) BuildArchitectPrompt(ctx *sharedctx.SharedContext) string {
	return fmt.Sprintf(`Analyze this user request and design a complete system architecture.

User Request: "%s"

Think step by step about:
1. What are the core features and requirements?
2. What tech stack best serves these requirements?
3. What is the optimal folder/module structure?
4. What database schema is needed?
5. What API endpoints are required?
6. What authentication strategy is appropriate?
7. What deployment configuration makes sense?

Respond with a valid JSON blueprint following this schema:
%s

Return ONLY the JSON object, no explanation or markdown.`, ctx.UserPrompt, BlueprintJSONSchema)
}

// BuildPlannerPrompt constructs a prompt for the Planner agent
func (b *Builder) BuildPlannerPrompt(ctx *sharedctx.SharedContext) string {
	bpJSON := "{}"
	if ctx.GetBlueprint() != nil {
		if data, err := json.MarshalIndent(ctx.GetBlueprint(), "", "  "); err == nil {
			bpJSON = string(data)
		}
	}

	return fmt.Sprintf(`Given this system blueprint, create an ordered implementation task graph.

User Request: "%s"

Blueprint:
%s

Create a dependency-ordered list of implementation tasks. Rules:
- Tasks must be ordered so dependencies are built first
- Each task should be concrete and implementable (1-3 files)
- Include specific prompts for the Builder agent for each task
- Foundation tasks first: models/schemas, then services, then APIs, then UI
- Auth must come before protected endpoints
- Database setup before models

Return a JSON array following this schema:
%s

Return ONLY the JSON array, no explanation or markdown.`, ctx.UserPrompt, bpJSON, TaskJSONSchema)
}

// BuildBuilderPrompt constructs a prompt for the Builder agent for a specific task
func (b *Builder) BuildBuilderPrompt(ctx *sharedctx.SharedContext, task *types.Task) string {
	bpJSON := "{}"
	if ctx.GetBlueprint() != nil {
		if data, err := json.MarshalIndent(ctx.GetBlueprint(), "", "  "); err == nil {
			bpJSON = string(data)
		}
	}

	existingFiles := ctx.GetAllFiles()

	var existingContext strings.Builder
	if len(existingFiles) > 0 {
		existingContext.WriteString("\nAlready generated files:\n")
		// Include content of up to 5 most relevant existing files
		count := 0
		for path, content := range existingFiles {
			if count >= 5 {
				break
			}
			existingContext.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", path, truncate(content, 500)))
			count++
		}
	}

	return fmt.Sprintf(`Generate complete, production-ready code for this implementation task.

User's Original Request: "%s"

System Blueprint:
%s
%s

Task: %s
Description: %s
Files to generate: %s

%s

Requirements:
- Write COMPLETE files, not stubs or placeholders
- Follow the tech stack in the blueprint exactly
- Handle errors properly
- Include proper logging where appropriate
- Make code production-ready
- Be consistent with any existing files

For each file, use this format:
=== FILE: <filepath> ===
<complete file content>
=== END FILE ===

Generate all required files now.`,
		ctx.UserPrompt,
		bpJSON,
		existingContext.String(),
		task.Title,
		task.Description,
		strings.Join(task.Files, ", "),
		task.AgentPrompt)
}

// BuildReviewerPrompt constructs a prompt for the Reviewer agent
func (b *Builder) BuildReviewerPrompt(ctx *sharedctx.SharedContext) string {
	bpJSON := "{}"
	if ctx.GetBlueprint() != nil {
		if data, err := json.MarshalIndent(ctx.GetBlueprint(), "", "  "); err == nil {
			bpJSON = string(data)
		}
	}

	files := ctx.GetAllFiles()
	var filesSummary strings.Builder
	for path, content := range files {
		filesSummary.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", path, truncate(content, 1000)))
	}

	return fmt.Sprintf(`Review the following generated code for a system built to this specification.

Original Request: "%s"

Blueprint:
%s

Generated Files:
%s

Review for:
1. Correctness - does the code implement the requirements correctly?
2. Security - are there vulnerabilities (SQL injection, XSS, auth bypass, etc.)?
3. Error handling - are errors properly handled and propagated?
4. Best practices - does it follow language/framework conventions?
5. Consistency - is the code consistent across files?
6. Missing pieces - are there critical gaps?

Return a JSON array of review notes:
[
  {
    "file": "filepath",
    "line": 42,
    "severity": "critical|warning|suggestion",
    "category": "security|correctness|style|performance|completeness",
    "message": "description of the issue",
    "suggestion": "how to fix it"
  }
]

Return ONLY the JSON array. If no issues found, return [].`,
		ctx.UserPrompt, bpJSON, filesSummary.String())
}

// BuildTesterPrompt constructs a prompt for the Tester agent
func (b *Builder) BuildTesterPrompt(ctx *sharedctx.SharedContext) string {
	files := ctx.GetAllFiles()
	var filesSummary strings.Builder
	for path, content := range files {
		filesSummary.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", path, truncate(content, 800)))
	}

	bp := ctx.GetBlueprint()
	lang := "Go"
	framework := ""
	if bp != nil {
		lang = bp.TechStack.Language
		framework = bp.TechStack.Framework
	}

	return fmt.Sprintf(`Generate comprehensive tests for the following code.

Language: %s
Framework: %s

Source Files:
%s

Generate:
1. Unit tests for each significant function/method
2. Integration test scaffolding for API endpoints
3. Edge case and error case tests
4. Mock/stub setup where needed

For each test file, use this format:
=== FILE: <test_filepath> ===
<complete test file content>
=== END FILE ===

Write COMPLETE, runnable test files. Cover happy paths, error paths, and edge cases.`,
		lang, framework, filesSummary.String())
}

// BuildIteratorPrompt constructs a prompt for the Iterator agent
func (b *Builder) BuildIteratorPrompt(ctx *sharedctx.SharedContext) string {
	feedback := ctx.GetLatestFeedback()

	bpJSON := "{}"
	if ctx.GetBlueprint() != nil {
		if data, err := json.MarshalIndent(ctx.GetBlueprint(), "", "  "); err == nil {
			bpJSON = string(data)
		}
	}

	files := ctx.GetAllFiles()
	fileList := make([]string, 0, len(files))
	for path := range files {
		fileList = append(fileList, path)
	}

	var filesSummary strings.Builder
	for path, content := range files {
		filesSummary.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", path, truncate(content, 600)))
	}

	return fmt.Sprintf(`The user wants to modify an existing generated system.

Original Request: "%s"
User Feedback: "%s"

Current System Blueprint:
%s

Current Files:
%s

%s

Analysis steps:
1. Which files are affected by this feedback?
2. What specific changes are needed in each file?
3. Are there cascading changes to other files?
4. What is the MINIMAL change set?

First, output a JSON analysis:
{
  "affected_files": ["filepath"],
  "changes": [
    {
      "file": "filepath",
      "reason": "why this file needs to change",
      "change_type": "modify|add|delete"
    }
  ]
}

Then, generate ONLY the changed files using this format:
=== FILE: <filepath> ===
<complete updated file content>
=== END FILE ===

Only output files that need to change. Maintain consistency with unchanged files.`,
		ctx.UserPrompt,
		feedback,
		bpJSON,
		strings.Join(fileList, "\n"),
		filesSummary.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... [truncated]"
}

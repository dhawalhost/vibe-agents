package agents

import (
	"context"
	"fmt"

	sharedctx "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/pkg/types"
)

const maxReviewRetries = 3

// OrchestratorAgent coordinates all other agents
type OrchestratorAgent struct {
	*BaseAgent
	architect *ArchitectAgent
	planner   *PlannerAgent
	builder   *BuilderAgent
	reviewer  *ReviewerAgent
	tester    *TesterAgent
	iterator  *IteratorAgent
}

func NewOrchestratorAgent(
	provider llm.LLMProvider,
	model string,
	architect *ArchitectAgent,
	planner *PlannerAgent,
	builder *BuilderAgent,
	reviewer *ReviewerAgent,
	tester *TesterAgent,
	iterator *IteratorAgent,
) *OrchestratorAgent {
	return &OrchestratorAgent{
		BaseAgent: NewBaseAgent(types.AgentOrchestrator, types.ReasoningReAct, provider, model),
		architect: architect,
		planner:   planner,
		builder:   builder,
		reviewer:  reviewer,
		tester:    tester,
		iterator:  iterator,
	}
}

func (o *OrchestratorAgent) Think(ctx context.Context, sharedCtx *sharedctx.SharedContext) ([]string, error) {
	thoughts := []string{
		fmt.Sprintf("Received user prompt: %q", sharedCtx.UserPrompt),
		"Phase 1: Routing to Architect for system design",
		"Phase 2: Will send blueprint to Planner for task decomposition",
		"Phase 3: Builder will generate code for each task",
		"Phase 4: Reviewer will validate generated code",
		"Phase 5: Tester will generate test suite",
		"Feedback loop: If critical issues found, rebuild affected files (max 3 retries)",
	}
	o.LogThoughts(sharedCtx, thoughts)
	return thoughts, nil
}

// Run executes the full agent pipeline
func (o *OrchestratorAgent) Run(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	// Think first
	if _, err := o.Think(ctx, sharedCtx); err != nil {
		return fmt.Errorf("orchestrator thinking: %w", err)
	}

	// Phase 1: Architecture
	fmt.Println("🏗  Architect: Designing system architecture...")
	if err := o.architect.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("architect failed: %w", err)
	}

	// Phase 2: Planning
	fmt.Println("📐 Planner: Creating implementation task graph...")
	if err := o.planner.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("planner failed: %w", err)
	}

	// Phase 3: Building
	fmt.Println("🧩 Builder: Generating code...")
	if err := o.builder.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("builder failed: %w", err)
	}

	// Phase 4: Review with retry loop
	for attempt := 0; attempt < maxReviewRetries; attempt++ {
		fmt.Printf("🔍 Reviewer: Reviewing code (attempt %d/%d)...\n", attempt+1, maxReviewRetries)
		sharedCtx.ClearReviewNotes()

		if err := o.reviewer.Act(ctx, sharedCtx); err != nil {
			return fmt.Errorf("reviewer failed: %w", err)
		}

		if !sharedCtx.HasCriticalIssues() {
			break
		}

		if attempt < maxReviewRetries-1 {
			fmt.Printf("⚠️  Critical issues found, triggering rebuild (attempt %d)...\n", attempt+1)
			if err := o.builder.Act(ctx, sharedCtx); err != nil {
				return fmt.Errorf("builder retry %d failed: %w", attempt+1, err)
			}
		}
	}

	// Phase 5: Testing
	fmt.Println("🧪 Tester: Generating test suite...")
	if err := o.tester.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("tester failed: %w", err)
	}

	fmt.Println("✅ Pipeline complete!")
	return nil
}

// Iterate handles user feedback with minimal rebuilds
func (o *OrchestratorAgent) Iterate(ctx context.Context, sharedCtx *sharedctx.SharedContext, feedback string) error {
	record := &types.IterationRecord{
		Round:    len(sharedCtx.IterationHistory) + 1,
		Feedback: feedback,
	}
	sharedCtx.AddIterationRecord(record)

	fmt.Printf("🔄 Iterator: Processing feedback: %q\n", feedback)
	if err := o.iterator.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("iterator failed: %w", err)
	}

	// Re-review after iteration
	fmt.Println("🔍 Reviewer: Re-reviewing after iteration...")
	sharedCtx.ClearReviewNotes()
	if err := o.reviewer.Act(ctx, sharedCtx); err != nil {
		return fmt.Errorf("post-iteration review failed: %w", err)
	}

	fmt.Println("✅ Iteration complete!")
	return nil
}

func (o *OrchestratorAgent) Act(ctx context.Context, sharedCtx *sharedctx.SharedContext) error {
	return o.Run(ctx, sharedCtx)
}

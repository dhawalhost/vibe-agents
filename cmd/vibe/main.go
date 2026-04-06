package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dhawalhost/vibe-agents/internal/agents"
	"github.com/dhawalhost/vibe-agents/internal/config"
	vibecontext "github.com/dhawalhost/vibe-agents/internal/context"
	"github.com/dhawalhost/vibe-agents/internal/llm"
	"github.com/dhawalhost/vibe-agents/internal/output"
	"github.com/dhawalhost/vibe-agents/internal/server"
)

var (
	cfgFile   string
	outputDir string
	overwrite bool
	model     string
	provider  string
	stream    bool
	servePort int
	noOpen    bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "vibe",
		Short: "Vibe Agents - Multi-agent AI vibe coding system",
		Long: `Vibe Agents is a multi-agent AI system that generates complete, working systems
from high-level natural language prompts. It uses a pipeline of specialized agents:

  🎯 Orchestrator → 🏗 Architect → 📐 Planner → 🧩 Builder → 🔍 Reviewer → 🧪 Tester
                                                                              ↕
                                                                      🔄 Iterator (feedback)

GitHub Copilot is used as the primary LLM provider.
Set GITHUB_TOKEN environment variable to authenticate.`,
		Version: "1.0.0",
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.vibe-agents/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&provider, "provider", "", "LLM provider (copilot, openai, anthropic, ollama)")
	rootCmd.PersistentFlags().StringVar(&model, "model", "", "LLM model to use (e.g., gpt-4o, claude-sonnet-4)")

	// generate command
	generateCmd := &cobra.Command{
		Use:   "generate [prompt]",
		Short: "Generate a complete system from a vibe prompt",
		Long:  `Generate a complete, working system from a natural language description.`,
		Args:  cobra.ExactArgs(1),
		Example: `  vibe generate "Build a REST API with user auth and PostgreSQL"
  vibe generate "Build a React dashboard with real-time data" --output ./my-dashboard
  vibe generate "SaaS with billing and auth" --model gpt-4o --output ./saas-app`,
		RunE: runGenerate,
	}
	generateCmd.Flags().StringVarP(&outputDir, "output", "o", "./output", "output directory for generated files")
	generateCmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing files")
	generateCmd.Flags().BoolVar(&stream, "stream", false, "stream LLM output in real-time")

	// iterate command
	iterateCmd := &cobra.Command{
		Use:   "iterate [feedback]",
		Short: "Iterate on an existing generated project",
		Long:  `Apply user feedback to an existing generated project with minimal regeneration.`,
		Args:  cobra.ExactArgs(1),
		Example: `  vibe iterate "Add dark mode support" --project ./my-app
  vibe iterate "Switch from PostgreSQL to MongoDB" --project ./my-api`,
		RunE: runIterate,
	}
	iterateCmd.Flags().StringP("project", "p", "./output", "path to the existing generated project")

	// explain command
	explainCmd := &cobra.Command{
		Use:     "explain",
		Short:   "Show the chain of thought reasoning from last generation",
		Long:    `Display the full chain-of-thought reasoning log from the last generation run.`,
		Example: `  vibe explain --project ./my-app`,
		RunE:    runExplain,
	}
	explainCmd.Flags().StringP("project", "p", "./output", "path to the existing generated project")

	// config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage vibe-agents configuration",
	}

	configSetCmd := &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		Example: `  vibe config set provider copilot
  vibe config set model gpt-4o`,
		RunE: runConfigSet,
	}

	configGetCmd := &cobra.Command{
		Use:     "get [key]",
		Short:   "Get a configuration value",
		Args:    cobra.ExactArgs(1),
		Example: `  vibe config get provider`,
		RunE:    runConfigGet,
	}

	configCmd.AddCommand(configSetCmd, configGetCmd)

	// serve command
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Launch the Vibe Agents web UI",
		Long: `Start an HTTP server that provides a browser-based UI for running the
agent pipeline, viewing generated files, reviewing code, and iterating.`,
		Example: `  vibe serve
  vibe serve --port 3000
  vibe serve --no-open`,
		RunE: runServe,
	}
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to listen on")
	serveCmd.Flags().BoolVar(&noOpen, "no-open", false, "do not automatically open the browser")

	rootCmd.AddCommand(generateCmd, iterateCmd, explainCmd, configCmd, serveCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// buildCopilotProvider creates a CopilotProvider using either GitHub App
// installation credentials (preferred) or a static GITHUB_TOKEN.
func buildCopilotProvider() (*llm.CopilotProvider, error) {
	return llm.BuildCopilotProvider(
		config.GetGitHubAppID(), config.GetGitHubAppPrivateKey(),
		config.GetGitHubAppInstallationID(), config.GetGitHubToken(),
	)
}

func buildPipeline(cfg *config.Config) (*agents.OrchestratorAgent, error) {
	// Determine provider
	providerName := cfg.Provider
	if provider != "" {
		providerName = provider
	}

	// Determine model
	modelName := cfg.Model
	if model != "" {
		modelName = model
	}

	// Create router
	router := llm.NewProviderRouter(providerName, modelName)

	// Register providers
	switch providerName {
	case "copilot":
		copilotProv, err := buildCopilotProvider()
		if err != nil {
			return nil, err
		}
		router.Register("copilot", copilotProv)
	case "openai":
		apiKey := config.GetOpenAIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		router.Register("openai", llm.NewOpenAIProvider(apiKey))
	case "anthropic":
		apiKey := config.GetAnthropicKey()
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		router.Register("anthropic", llm.NewAnthropicProvider(apiKey))
	case "ollama":
		router.Register("ollama", llm.NewOllamaProvider("http://localhost:11434"))
	default:
		// Try copilot as fallback
		copilotProv, err := buildCopilotProvider()
		if err != nil {
			return nil, fmt.Errorf("unknown provider %q and no valid Copilot credentials set: %w", providerName, err)
		}
		router.Register("copilot", copilotProv)
		providerName = "copilot"
	}

	// Get the primary provider for agents
	prov, mod, err := router.GetProvider("")
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	if modelName != "" {
		mod = modelName
	}

	// Use the selected model consistently across all agents for a predictable UX.
	pickModel := func(agentCfgModel string) string {
		if modelName != "" {
			return modelName
		}
		if agentCfgModel != "" {
			return agentCfgModel
		}
		return mod
	}

	architectModel := pickModel(cfg.Agents.Architect.Model)
	plannerModel := pickModel(cfg.Agents.Planner.Model)
	builderModel := pickModel(cfg.Agents.Builder.Model)
	reviewerModel := pickModel(cfg.Agents.Reviewer.Model)
	testerModel := pickModel(cfg.Agents.Tester.Model)
	iteratorModel := pickModel(cfg.Agents.Iterator.Model)

	// Create agents
	architect := agents.NewArchitectAgent(prov, architectModel)
	planner := agents.NewPlannerAgent(prov, plannerModel)
	builder := agents.NewBuilderAgent(prov, builderModel)
	reviewer := agents.NewReviewerAgent(prov, reviewerModel)
	tester := agents.NewTesterAgent(prov, testerModel)
	iterator := agents.NewIteratorAgent(prov, iteratorModel)

	orchestrator := agents.NewOrchestratorAgent(
		prov, mod,
		architect, planner, builder, reviewer, tester, iterator,
	)

	return orchestrator, nil
}

func runGenerate(cmd *cobra.Command, args []string) error {
	userPrompt := args[0]

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if outputDir != "" {
		cfg.Output.Directory = outputDir
	}
	if overwrite {
		cfg.Output.Overwrite = true
	}

	orchestrator, err := buildPipeline(cfg)
	if err != nil {
		return err
	}

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Received interrupt signal, shutting down gracefully...")
		cancel()
	}()

	sharedCtx := vibecontext.New(userPrompt)
	sharedCtx.OutputDir = cfg.Output.Directory

	fmt.Printf("🚀 Vibe Agents: Generating system for prompt:\n   %q\n\n", userPrompt)

	if err := orchestrator.Run(ctx, sharedCtx); err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Write files to disk
	writer := output.New(cfg.Output.Directory, cfg.Output.Overwrite)
	if err := writer.WriteAll(sharedCtx); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	// Save context for future iterations
	if err := writer.SaveContext(sharedCtx); err != nil {
		fmt.Printf("Warning: could not save context: %v\n", err)
	}

	// Print report
	report := writer.GenerateReport(sharedCtx)
	fmt.Println("\n" + report)

	return nil
}

func runIterate(cmd *cobra.Command, args []string) error {
	feedback := args[0]
	projectDir, _ := cmd.Flags().GetString("project")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Load existing context
	sharedCtx, err := output.LoadContext(projectDir)
	if err != nil {
		return fmt.Errorf("load project context from %q: %w\n(Run 'vibe generate' first to create a project)", projectDir, err)
	}

	orchestrator, err := buildPipeline(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Received interrupt signal, shutting down gracefully...")
		cancel()
	}()

	fmt.Printf("🔄 Iterating on project at %q\n   Feedback: %q\n\n", projectDir, feedback)

	if err := orchestrator.Iterate(ctx, sharedCtx, feedback); err != nil {
		return fmt.Errorf("iteration failed: %w", err)
	}

	// Write updated files
	writer := output.New(projectDir, true) // Always overwrite on iteration
	if err := writer.WriteAll(sharedCtx); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	// Save updated context
	if err := writer.SaveContext(sharedCtx); err != nil {
		fmt.Printf("Warning: could not save context: %v\n", err)
	}

	fmt.Printf("\n✅ Iteration complete! Updated project at %q\n", projectDir)
	return nil
}

func runExplain(cmd *cobra.Command, args []string) error {
	projectDir, _ := cmd.Flags().GetString("project")

	sharedCtx, err := output.LoadContext(projectDir)
	if err != nil {
		return fmt.Errorf("load project context from %q: %w", projectDir, err)
	}

	thoughts := sharedCtx.GetChainOfThought()
	if len(thoughts) == 0 {
		fmt.Println("No chain of thought recorded for this project.")
		return nil
	}

	fmt.Printf("🧠 Chain of Thought for: %q\n\n", sharedCtx.UserPrompt)
	currentAgent := ""
	for _, thought := range thoughts {
		if string(thought.Agent) != currentAgent {
			currentAgent = string(thought.Agent)
			fmt.Printf("\n[%s]\n", currentAgent)
		}
		fmt.Printf("  %d. %s\n", thought.Step, thought.Thought)
	}

	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	if err := config.Save(key, value); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("✅ Set %s = %s\n", key, value)
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	_ = cfg
	fmt.Printf("Config key: %s\n", args[0])
	fmt.Println("(Use 'vibe config set' to modify values)")
	return nil
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	providerName := cfg.Provider
	if provider != "" {
		providerName = provider
	}
	modelName := cfg.Model
	if model != "" {
		modelName = model
	}

	srv := server.New(server.Options{
		Port:            servePort,
		NoOpen:          noOpen,
		DefaultProvider: providerName,
		DefaultModel:    modelName,
		Config:          cfg,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Shutting down server…")
		cancel()
	}()

	return srv.Run(ctx)
}

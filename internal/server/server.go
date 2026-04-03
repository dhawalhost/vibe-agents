// Package server provides the HTTP server for the vibe-agents web UI.
//
// Usage:
//
//	srv := server.New(server.Options{Port: 8080, ...})
//	srv.Run(ctx)
package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dhawalhost/vibe-agents/internal/agents"
	"github.com/dhawalhost/vibe-agents/internal/config"
	"github.com/dhawalhost/vibe-agents/internal/llm"
)

//go:embed web
var webFS embed.FS

// Options configure the HTTP server.
type Options struct {
	Port            int
	NoOpen          bool
	DefaultProvider string
	DefaultModel    string
	Config          *config.Config
}

// Server is the vibe-agents HTTP server.
type Server struct {
	opts            Options
	store           *JobStore
	defaultProvider string
	defaultModel    string
	cfg             *config.Config
}

// New creates a new Server.
func New(opts Options) *Server {
	return &Server{
		opts:            opts,
		store:           newJobStore(),
		defaultProvider: opts.DefaultProvider,
		defaultModel:    opts.DefaultModel,
		cfg:             opts.Config,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (srv *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Static web assets (index.html, app.js, style.css)
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("embed web fs: %w", err)
	}
	fileServer := http.FileServer(http.FS(sub))

	// API routes
	mux.HandleFunc("/api/generate", srv.handleGenerate)
	mux.HandleFunc("/api/events/", srv.handleSSE)
	mux.HandleFunc("/api/jobs/", srv.routeJobHandler)

	// SPA catch-all — serve index.html for any non-API, non-static path
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve static assets directly (app.js, style.css, etc.)
		if r.URL.Path != "/" {
			// Check if the file exists; if so serve it.
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Otherwise serve the SPA shell.
		fileServer.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", srv.opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	httpSrv := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}

	url := fmt.Sprintf("http://localhost:%d", srv.opts.Port)
	fmt.Printf("🌐 Vibe Agents UI running at %s\n", url)

	if !srv.opts.NoOpen {
		go openBrowser(url)
	}

	// Shut down gracefully when ctx is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// routeJobHandler dispatches /api/jobs/* sub-paths.
func (srv *Server) routeJobHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path // e.g. /api/jobs/{id}  or /api/jobs/{id}/files  or /api/jobs/{id}/files/{path}

	rest := strings.TrimPrefix(path, "/api/jobs/")
	parts := strings.SplitN(rest, "/", 2)

	if len(parts) == 1 {
		// /api/jobs/{id}
		srv.handleGetJob(w, r)
		return
	}

	sub := parts[1] // "files", "files/{path}", "iterate", "context"
	switch {
	case sub == "files":
		srv.handleListFiles(w, r)
	case strings.HasPrefix(sub, "files/"):
		srv.handleGetFile(w, r)
	case sub == "iterate":
		srv.handleIterate(w, r)
	case sub == "context":
		srv.handleGetContext(w, r)
	default:
		http.NotFound(w, r)
	}
}

// buildCopilotProvider creates a CopilotProvider using either GitHub App
// installation credentials (preferred) or a static GITHUB_TOKEN.
func buildCopilotProvider() (*llm.CopilotProvider, error) {
	appID := config.GetGitHubAppID()
	privateKeyPEM := config.GetGitHubAppPrivateKey()
	installationID := config.GetGitHubAppInstallationID()

	if appID != "" && privateKeyPEM != "" {
		// installationID is optional: if empty it will be auto-discovered.
		ts, err := llm.NewGitHubAppTokenSource(appID, privateKeyPEM, installationID)
		if err != nil {
			return nil, fmt.Errorf("create GitHub App token source: %w", err)
		}
		return llm.NewCopilotProviderWithTokenSource(ts), nil
	}

	token := config.GetGitHubToken()
	if token == "" {
		return nil, fmt.Errorf(
			"Copilot provider requires either:\n" +
				"  • GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY (or GITHUB_APP_PRIVATE_KEY_PATH)\n" +
				"  • GITHUB_TOKEN (OAuth token from `gh auth token`)",
		)
	}
	return llm.NewCopilotProvider(token), nil
}

// buildPipeline creates a fully-wired OrchestratorAgent for a given provider/model.
func (srv *Server) buildPipeline(providerName, modelName string) (*agents.OrchestratorAgent, error) {
	cfg := srv.cfg
	router := llm.NewProviderRouter(providerName, modelName)

	switch providerName {
	case "copilot":
		copilotProv, err := buildCopilotProvider()
		if err != nil {
			return nil, err
		}
		router.Register("copilot", copilotProv)
	case "openai":
		key := config.GetOpenAIKey()
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		router.Register("openai", llm.NewOpenAIProvider(key))
	case "anthropic":
		key := config.GetAnthropicKey()
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		router.Register("anthropic", llm.NewAnthropicProvider(key))
	case "ollama":
		router.Register("ollama", llm.NewOllamaProvider("http://localhost:11434"))
	default:
		copilotProv, err := buildCopilotProvider()
		if err != nil {
			return nil, fmt.Errorf("unknown provider %q and no valid Copilot credentials set: %w", providerName, err)
		}
		router.Register("copilot", copilotProv)
		providerName = "copilot"
	}

	prov, mod, err := router.GetProvider("")
	if err != nil {
		return nil, err
	}
	if modelName != "" {
		mod = modelName
	}

	// Per-agent model overrides from config.
	pick := func(agentCfgModel string) string {
		if agentCfgModel != "" {
			return agentCfgModel
		}
		return mod
	}

	architect := agents.NewArchitectAgent(prov, pick(cfg.Agents.Architect.Model))
	planner := agents.NewPlannerAgent(prov, pick(cfg.Agents.Planner.Model))
	builder := agents.NewBuilderAgent(prov, pick(cfg.Agents.Builder.Model))
	reviewer := agents.NewReviewerAgent(prov, pick(cfg.Agents.Reviewer.Model))
	tester := agents.NewTesterAgent(prov, pick(cfg.Agents.Tester.Model))
	iterator := agents.NewIteratorAgent(prov, pick(cfg.Agents.Iterator.Model))

	return agents.NewOrchestratorAgent(prov, mod,
		architect, planner, builder, reviewer, tester, iterator,
	), nil
}

// openBrowser opens the given URL in the system default browser.
func openBrowser(url string) {
	// Small delay to let the server start.
	time.Sleep(500 * time.Millisecond)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

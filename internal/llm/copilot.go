package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	CopilotBaseURL    = "https://api.githubcopilot.com"
	CopilotAPIVersion = "2023-07-07"

	// copilotProviderConfigError is the error message surfaced when neither
	// GitHub App credentials nor a static token are configured.
	copilotProviderConfigError = "Copilot provider requires either:\n" +
		"  • GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY (or GITHUB_APP_PRIVATE_KEY_PATH)\n" +
		"  • GITHUB_TOKEN (OAuth token from `gh auth token`)"
)

// copilotTransport is an http.RoundTripper that injects the Copilot-specific
// request headers and the bearer token obtained from a TokenSource.
type copilotTransport struct {
	tokenSource TokenSource
	base        http.RoundTripper
}

// failoverTokenSource tries primary first and falls back to secondary if
// primary fails (for example, if a GitHub App installation token refresh
// returns 401 due to a revoked key or installation access change).
type failoverTokenSource struct {
	primary   TokenSource
	secondary TokenSource
}

func (s *failoverTokenSource) Token(ctx context.Context) (string, error) {
	token, err := s.primary.Token(ctx)
	if err == nil {
		return token, nil
	}

	fallbackToken, fallbackErr := s.secondary.Token(ctx)
	if fallbackErr != nil {
		return "", fmt.Errorf("primary token source failed: %w; fallback token source failed: %v", err, fallbackErr)
	}
	return fallbackToken, nil
}

func (t *copilotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenSource.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("get Copilot token: %w", err)
	}
	// Clone the request before mutating headers (required by RoundTripper contract).
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Copilot-Integration-Id", "vibe-agents")
	req.Header.Set("Editor-Version", "vibe-agents/1.0.0")
	req.Header.Set("User-Agent", "vibe-agents/1.0.0")
	req.Header.Set("OpenAI-Intent", "conversation-panel")
	return t.base.RoundTrip(req)
}

// BuildCopilotProvider creates a CopilotProvider from explicit credential values.
// It prefers a static OAuth token when available (GITHUB_TOKEN / gh auth token)
// because this is the most broadly accepted credential for Copilot endpoints.
// If no OAuth token is available, it falls back to GitHub App credentials
// (appID + privateKeyPEM required; installationID optional and auto-discovered
// when empty). Returns a descriptive error if neither set of credentials is usable.
//
// This is the canonical credential-resolution helper; both the CLI and the HTTP
// server call it so that auth behaviour stays in sync.
func BuildCopilotProvider(appID, privateKeyPEM, installationID, token string) (*CopilotProvider, error) {
	if token != "" {
		return NewCopilotProvider(token), nil
	}

	if appID != "" && privateKeyPEM != "" {
		// installationID is optional: if empty it will be auto-discovered.
		ts, err := NewGitHubAppTokenSource(appID, privateKeyPEM, installationID)
		if err != nil {
			return nil, fmt.Errorf("create GitHub App token source: %w", err)
		}
		return NewCopilotProviderWithTokenSource(ts), nil
	}

	return nil, fmt.Errorf(copilotProviderConfigError)
}

// CopilotProvider implements LLMProvider for GitHub Copilot.
// It uses the go-openai SDK pointed at the Copilot API endpoint, with a custom
// transport that supplies the dynamic bearer token and Copilot-specific headers.
type CopilotProvider struct {
	client *openai.Client
	models []string

	mu        sync.RWMutex
	autoModel string
}

// NewCopilotProvider creates a new GitHub Copilot provider using a static token
// (e.g. a GITHUB_TOKEN OAuth token obtained via `gh auth token`).
func NewCopilotProvider(token string) *CopilotProvider {
	return NewCopilotProviderWithTokenSource(NewStaticTokenSource(token))
}

// NewCopilotProviderWithTokenSource creates a new GitHub Copilot provider that
// obtains its bearer token from the given TokenSource. Use this with
// GitHubAppTokenSource to authenticate via a GitHub App installation.
func NewCopilotProviderWithTokenSource(ts TokenSource) *CopilotProvider {
	cfg := openai.DefaultConfig("") // auth is handled by the transport
	cfg.BaseURL = CopilotBaseURL
	cfg.HTTPClient = &http.Client{
		Timeout: 45 * time.Second,
		Transport: &copilotTransport{
			tokenSource: ts,
			base:        http.DefaultTransport,
		},
	}
	return &CopilotProvider{
		client: openai.NewClientWithConfig(cfg),
		models: []string{
			"auto",
			"gpt-4o-mini-2024-07-18",
			"gpt-4.1",
			"gpt-4o-mini",
			"gpt-5-mini",
			"claude-sonnet-4",
			"claude-3.5-sonnet",
			"gpt-5.4", "gpt-5.4-mini", "gpt-5.1-codex", "gpt-5.2-codex",
			"claude-opus-4.5", "claude-sonnet-4.0", "claude-sonnet-4.5",
		},
	}
}

func (p *CopilotProvider) Name() string     { return "copilot" }
func (p *CopilotProvider) Models() []string { return p.models }

func (p *CopilotProvider) buildMessages(req LLMRequest) []openai.ChatCompletionMessage {
	var messages []openai.ChatCompletionMessage
	if req.SystemPrompt != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return messages
}

func isRetryableCopilotModelError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "requested model is not supported") ||
		(strings.Contains(s, "400") && strings.Contains(s, "model") && strings.Contains(s, "not supported")) ||
		(strings.Contains(s, "403") && strings.Contains(s, "access to this endpoint is forbidden")) ||
		(strings.Contains(s, "422") && strings.Contains(s, "unprocessable entity"))
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "client.timeout exceeded") ||
		strings.Contains(s, "timeout")
}

func dedupeModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func normalizeCopilotModel(requested string) string {
	trimmed := strings.TrimSpace(requested)
	switch strings.ToLower(trimmed) {
	case "", "auto", "copilot-auto", "default":
		return "auto"
	case "gpt-4o", "gpt-4o-mini":
		return "gpt-4o-mini-2024-07-18"
	default:
		return trimmed
	}
}

func preferredCopilotModels() []string {
	return []string{
		"gpt-4o-mini-2024-07-18",
		"gpt-4.1",
		"gpt-4o-mini",
		"claude-sonnet-4",
		"claude-3.5-sonnet",
	}
}

func (p *CopilotProvider) rememberedAutoModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.autoModel
}

func (p *CopilotProvider) rememberWorkingModel(model string) {
	model = normalizeCopilotModel(model)
	if model == "" || model == "auto" {
		return
	}
	p.mu.Lock()
	p.autoModel = model
	p.mu.Unlock()
}

func (p *CopilotProvider) resolveModel(ctx context.Context, requested string) string {
	model := normalizeCopilotModel(requested)
	if model != "auto" {
		return model
	}
	if remembered := p.rememberedAutoModel(); remembered != "" {
		return remembered
	}

	preferred := preferredCopilotModels()
	if p.client != nil {
		if list, err := p.client.ListModels(ctx); err == nil && len(list.Models) > 0 {
			available := make(map[string]struct{}, len(list.Models))
			for _, m := range list.Models {
				available[strings.TrimSpace(m.ID)] = struct{}{}
			}
			for _, candidate := range preferred {
				if _, ok := available[candidate]; ok {
					p.rememberWorkingModel(candidate)
					return candidate
				}
			}
		}
	}

	p.rememberWorkingModel(preferred[0])
	return preferred[0]
}

func needsMaxCompletionTokens(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "gpt-5") || strings.HasPrefix(m, "o1")
}

func applyCopilotTokenLimit(r *openai.ChatCompletionRequest, maxTokens int) {
	if maxTokens <= 0 {
		r.MaxTokens = 0
		r.MaxCompletionTokens = 0
		return
	}
	if needsMaxCompletionTokens(r.Model) {
		r.MaxCompletionTokens = maxTokens
		r.MaxTokens = 0
		return
	}
	r.MaxTokens = maxTokens
	r.MaxCompletionTokens = 0
}

func needsFixedSampling(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "gpt-5")
}

func applyCopilotTemperature(r *openai.ChatCompletionRequest, temperature float64) {
	if needsFixedSampling(r.Model) {
		r.Temperature = 0
		return
	}
	if temperature > 0 {
		r.Temperature = float32(temperature)
		return
	}
	r.Temperature = 0
}

func applyCopilotRequestSettings(r *openai.ChatCompletionRequest, req LLMRequest) {
	applyCopilotTokenLimit(r, req.MaxTokens)
	applyCopilotTemperature(r, req.Temperature)
}

func (p *CopilotProvider) fallbackModels(ctx context.Context, requested string) []string {
	requested = normalizeCopilotModel(requested)
	preferred := preferredCopilotModels()

	// If live provider capabilities are available, keep only the fast curated models
	// that are actually exposed for this account. Avoid broad retries into slower or
	// request-shape-sensitive models like gpt-5-mini, which can return plain-text 422s
	// for some Copilot chat requests and degrade UX.
	if p.client != nil {
		if list, err := p.client.ListModels(ctx); err == nil && len(list.Models) > 0 {
			available := make(map[string]struct{}, len(list.Models))
			for _, m := range list.Models {
				available[m.ID] = struct{}{}
			}
			filtered := make([]string, 0, len(preferred))
			for _, m := range preferred {
				if m == requested {
					continue
				}
				if _, ok := available[m]; ok {
					filtered = append(filtered, m)
				}
			}
			if len(filtered) > 0 {
				return dedupeModels(filtered)
			}
		}
	}

	filtered := make([]string, 0, len(preferred))
	for _, m := range preferred {
		if m == requested {
			continue
		}
		filtered = append(filtered, m)
	}
	return dedupeModels(filtered)
}

func (p *CopilotProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	req.Model = p.resolveModel(ctx, req.Model)

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
		N:        1,
	}
	applyCopilotRequestSettings(&r, req)

	resp, err := p.client.CreateChatCompletion(ctx, r)
	if err != nil {
		if !isRetryableCopilotModelError(err) {
			return nil, fmt.Errorf("copilot: %w", err)
		}

		var lastErr error = err
		for _, candidate := range p.fallbackModels(ctx, req.Model) {
			r.Model = candidate
			applyCopilotRequestSettings(&r, req)
			resp, err = p.client.CreateChatCompletion(ctx, r)
			if err == nil {
				p.rememberWorkingModel(candidate)
				break
			}
			lastErr = err
			if isTimeoutError(err) {
				return nil, fmt.Errorf("copilot: fallback model %q timed out: %w", candidate, err)
			}
			if !isRetryableCopilotModelError(err) {
				return nil, fmt.Errorf("copilot: fallback model %q failed: %w", candidate, err)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("copilot: requested model %q unsupported and no fallback model worked: %w", req.Model, lastErr)
		}
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("copilot: no choices in response")
	}
	p.rememberWorkingModel(resp.Model)
	return &LLMResponse{
		Content:    resp.Choices[0].Message.Content,
		Model:      resp.Model,
		Provider:   p.Name(),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

func (p *CopilotProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	req.Model = p.resolveModel(ctx, req.Model)

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
		Stream:   true,
		N:        1,
	}
	applyCopilotRequestSettings(&r, req)

	stream, err := p.client.CreateChatCompletionStream(ctx, r)
	if err != nil {
		if !isRetryableCopilotModelError(err) {
			return nil, fmt.Errorf("copilot: create stream: %w", err)
		}

		var lastErr error = err
		for _, candidate := range p.fallbackModels(ctx, req.Model) {
			r.Model = candidate
			applyCopilotRequestSettings(&r, req)
			stream, err = p.client.CreateChatCompletionStream(ctx, r)
			if err == nil {
				p.rememberWorkingModel(candidate)
				break
			}
			lastErr = err
			if isTimeoutError(err) {
				return nil, fmt.Errorf("copilot: fallback stream model %q timed out: %w", candidate, err)
			}
			if !isRetryableCopilotModelError(err) {
				return nil, fmt.Errorf("copilot: fallback stream model %q failed: %w", candidate, err)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("copilot: requested model %q unsupported and no fallback stream model worked: %w", req.Model, lastErr)
		}
	}

	p.rememberWorkingModel(r.Model)
	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				ch <- StreamChunk{Done: true}
				return
			}
			if err != nil {
				ch <- StreamChunk{Error: fmt.Errorf("copilot stream: %w", err)}
				return
			}
			if len(resp.Choices) > 0 {
				content := resp.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- StreamChunk{Content: content}:
					case <-ctx.Done():
						ch <- StreamChunk{Error: ctx.Err()}
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

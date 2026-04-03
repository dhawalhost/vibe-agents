package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	CopilotBaseURL    = "https://api.githubcopilot.com"
	CopilotAPIVersion = "2023-07-07"
)

// copilotTransport is an http.RoundTripper that injects the Copilot-specific
// request headers and the bearer token obtained from a TokenSource.
type copilotTransport struct {
	tokenSource TokenSource
	base        http.RoundTripper
}

func (t *copilotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("get Copilot token: %w", err)
	}
	// Clone the request before mutating headers (required by RoundTripper contract).
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Copilot-Integration-Id", "vibe-agents")
	req.Header.Set("Editor-Version", "vibe-agents/1.0.0")
	req.Header.Set("User-Agent", "vibe-agents/1.0.0")
	return t.base.RoundTrip(req)
}

// CopilotProvider implements LLMProvider for GitHub Copilot.
// It uses the go-openai SDK pointed at the Copilot API endpoint, with a custom
// transport that supplies the dynamic bearer token and Copilot-specific headers.
type CopilotProvider struct {
	client *openai.Client
	models []string
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
		Timeout: 120 * time.Second,
		Transport: &copilotTransport{
			tokenSource: ts,
			base:        http.DefaultTransport,
		},
	}
	return &CopilotProvider{
		client: openai.NewClientWithConfig(cfg),
		models: []string{
			"gpt-4o",
			"gpt-4o-mini",
			"claude-sonnet-4",
			"claude-3.5-sonnet",
			"o1-preview",
			"o1-mini",
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

func (p *CopilotProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
		N:        1,
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		r.Temperature = float32(req.Temperature)
	}

	resp, err := p.client.CreateChatCompletion(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("copilot: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("copilot: no choices in response")
	}
	return &LLMResponse{
		Content:    resp.Choices[0].Message.Content,
		Model:      resp.Model,
		Provider:   p.Name(),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

func (p *CopilotProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
		Stream:   true,
		N:        1,
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		r.Temperature = float32(req.Temperature)
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("copilot: create stream: %w", err)
	}

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

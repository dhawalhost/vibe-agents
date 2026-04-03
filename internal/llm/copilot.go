package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	CopilotAPIEndpoint = "https://api.githubcopilot.com/chat/completions"
	CopilotAPIVersion  = "2023-07-07"
)

// CopilotProvider implements LLMProvider for GitHub Copilot
type CopilotProvider struct {
	tokenSource TokenSource
	httpClient  *http.Client
	models      []string
}

// NewCopilotProvider creates a new GitHub Copilot provider using a static token
// (e.g. a GITHUB_TOKEN OAuth token obtained via `gh auth token`).
func NewCopilotProvider(token string) *CopilotProvider {
	return NewCopilotProviderWithTokenSource(NewStaticTokenSource(token))
}

// NewCopilotProviderWithTokenSource creates a new GitHub Copilot provider that
// obtains its bearer token from the given TokenSource.  Use this with
// GitHubAppTokenSource to authenticate via a GitHub App installation.
func NewCopilotProviderWithTokenSource(ts TokenSource) *CopilotProvider {
	return &CopilotProvider{
		tokenSource: ts,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
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

func (p *CopilotProvider) Name() string {
	return "copilot"
}

func (p *CopilotProvider) Models() []string {
	return p.models
}

type copilotRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
	N           int       `json:"n,omitempty"`
}

type copilotResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type copilotStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (p *CopilotProvider) buildMessages(req LLMRequest) []Message {
	var messages []Message
	if req.SystemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, req.Messages...)
	return messages
}

func (p *CopilotProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	payload := copilotRequest{
		Model:       req.Model,
		Messages:    p.buildMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
		N:           1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, CopilotAPIEndpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := p.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var copResp copilotResponse
	if err := json.Unmarshal(body, &copResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(copResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &LLMResponse{
		Content:    copResp.Choices[0].Message.Content,
		Model:      copResp.Model,
		Provider:   p.Name(),
		TokensUsed: copResp.Usage.TotalTokens,
	}, nil
}

func (p *CopilotProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 4096
	}

	payload := copilotRequest{
		Model:       req.Model,
		Messages:    p.buildMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
		N:           1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, CopilotAPIEndpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if err := p.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}

			var chunk copilotStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- StreamChunk{Content: content}:
					case <-ctx.Done():
						ch <- StreamChunk{Error: ctx.Err()}
						return
					}
				}
				if chunk.Choices[0].FinishReason != nil {
					ch <- StreamChunk{Done: true}
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch, nil
}

func (p *CopilotProvider) setHeaders(req *http.Request) error {
	token, err := p.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("get Copilot token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Copilot-Integration-Id", "vibe-agents")
	req.Header.Set("Editor-Version", "vibe-agents/1.0.0")
	req.Header.Set("User-Agent", "vibe-agents/1.0.0")
	return nil
}

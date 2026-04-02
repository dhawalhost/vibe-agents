package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const AnthropicAPIEndpoint = "https://api.anthropic.com/v1/messages"

// AnthropicProvider implements LLMProvider for Anthropic Claude
type AnthropicProvider struct {
	apiKey     string
	httpClient *http.Client
	models     []string
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		models:     []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5"},
	}
}

func (p *AnthropicProvider) Name() string    { return "anthropic" }
func (p *AnthropicProvider) Models() []string { return p.models }

func (p *AnthropicProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if req.Model == "" {
		req.Model = "claude-sonnet-4-5"
	}

	type anthropicMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []anthropicMessage
	for _, m := range req.Messages {
		messages = append(messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	payload := map[string]interface{}{
		"model":      req.Model,
		"messages":   messages,
		"max_tokens": 4096,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.SystemPrompt != "" {
		payload["system"] = req.SystemPrompt
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, AnthropicAPIEndpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

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

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	return &LLMResponse{
		Content:    result.Content[0].Text,
		Model:      result.Model,
		Provider:   p.Name(),
		TokensUsed: result.Usage.InputTokens + result.Usage.OutputTokens,
	}, nil
}

func (p *AnthropicProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	go func() {
		defer close(ch)
		resp, err := p.Generate(ctx, req)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}
		ch <- StreamChunk{Content: resp.Content, Done: true}
	}()
	return ch, nil
}

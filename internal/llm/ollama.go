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

// OllamaProvider implements LLMProvider for local Ollama instances
type OllamaProvider struct {
	baseURL    string
	httpClient *http.Client
	models     []string
}

func NewOllamaProvider(baseURL string) *OllamaProvider {
	return &OllamaProvider{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 300 * time.Second},
		models:     []string{"llama3", "mistral", "codellama", "gemma"},
	}
}

func (p *OllamaProvider) Name() string    { return "ollama" }
func (p *OllamaProvider) Models() []string { return p.models }

func (p *OllamaProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if req.Model == "" {
		req.Model = "llama3"
	}

	type ollamaMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []ollamaMessage
	if req.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, m := range req.Messages {
		messages = append(messages, ollamaMessage{Role: m.Role, Content: m.Content})
	}

	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": messages,
		"stream":   false,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.baseURL + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request (is Ollama running at %s?): %w", p.baseURL, err)
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
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &LLMResponse{
		Content:  result.Message.Content,
		Model:    result.Model,
		Provider: p.Name(),
	}, nil
}

func (p *OllamaProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
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

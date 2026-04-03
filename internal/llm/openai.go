package llm

import (
	"context"
	"errors"
	"fmt"
	"io"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements LLMProvider for OpenAI.
type OpenAIProvider struct {
	client *openai.Client
	models []string
}

func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		client: openai.NewClient(apiKey),
		models: []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"},
	}
}

func (p *OpenAIProvider) Name() string     { return "openai" }
func (p *OpenAIProvider) Models() []string { return p.models }

func (p *OpenAIProvider) buildMessages(req LLMRequest) []openai.ChatCompletionMessage {
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

func (p *OpenAIProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		r.Temperature = float32(req.Temperature)
	}

	resp, err := p.client.CreateChatCompletion(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}
	return &LLMResponse{
		Content:    resp.Choices[0].Message.Content,
		Model:      resp.Model,
		Provider:   p.Name(),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

func (p *OpenAIProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	if req.Model == "" {
		req.Model = "gpt-4o"
	}

	r := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: p.buildMessages(req),
		Stream:   true,
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		r.Temperature = float32(req.Temperature)
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("openai: create stream: %w", err)
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
				ch <- StreamChunk{Error: fmt.Errorf("openai stream: %w", err)}
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

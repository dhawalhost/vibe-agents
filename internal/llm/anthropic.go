package llm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const anthropicTimeout = 120 * time.Second

// AnthropicProvider implements LLMProvider for Anthropic Claude.
type AnthropicProvider struct {
	client *anthropic.Client
	models []string
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	c := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: anthropicTimeout}),
	)
	return &AnthropicProvider{
		client: &c,
		models: []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5"},
	}
}

func (p *AnthropicProvider) Name() string     { return "anthropic" }
func (p *AnthropicProvider) Models() []string { return p.models }

func (p *AnthropicProvider) buildParams(req LLMRequest) anthropic.MessageNewParams {
	maxTokens := int64(4096)
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}
	if req.Model == "" {
		req.Model = "claude-sonnet-4-5"
	}

	var messages []anthropic.MessageParam
	for _, m := range req.Messages {
		block := anthropic.NewTextBlock(m.Content)
		switch m.Role {
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(block))
		default:
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		Messages:  messages,
		MaxTokens: maxTokens,
	}
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt}}
	}
	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}
	return params
}

func (p *AnthropicProvider) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	msg, err := p.client.Messages.New(ctx, p.buildParams(req))
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("anthropic: no content in response")
	}

	var text string
	for _, block := range msg.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return &LLMResponse{
		Content:    text,
		Model:      string(msg.Model),
		Provider:   p.Name(),
		TokensUsed: int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
	}, nil
}

func (p *AnthropicProvider) GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)

		stream := p.client.Messages.NewStreaming(ctx, p.buildParams(req))
		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
				if event.Delta.Text != "" {
					select {
					case ch <- StreamChunk{Content: event.Delta.Text}:
					case <-ctx.Done():
						ch <- StreamChunk{Error: ctx.Err()}
						return
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("anthropic stream: %w", err)}
			return
		}
		ch <- StreamChunk{Done: true}
	}()
	return ch, nil
}

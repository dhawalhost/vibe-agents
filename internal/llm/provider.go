package llm

import (
	"context"
	"io"
)

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMRequest represents a request to an LLM provider
type LLMRequest struct {
	Model        string    `json:"model"`
	Messages     []Message `json:"messages"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	Temperature  float64   `json:"temperature,omitempty"`
	Stream       bool      `json:"stream,omitempty"`
	SystemPrompt string    `json:"-"` // Used to prepend a system message
}

// LLMResponse represents a response from an LLM provider
type LLMResponse struct {
	Content    string            `json:"content"`
	Model      string            `json:"model"`
	Provider   string            `json:"provider"`
	TokensUsed int               `json:"tokens_used,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// LLMProvider defines the interface for all LLM backends
type LLMProvider interface {
	Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	GenerateStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error)
	Name() string
	Models() []string
}

// StreamToString reads all stream chunks into a single string
func StreamToString(stream <-chan StreamChunk) (string, error) {
	var result string
	for chunk := range stream {
		if chunk.Error != nil {
			return result, chunk.Error
		}
		result += chunk.Content
		if chunk.Done {
			break
		}
	}
	return result, nil
}

// StreamToWriter writes stream chunks to an io.Writer
func StreamToWriter(stream <-chan StreamChunk, w io.Writer) error {
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		if _, err := io.WriteString(w, chunk.Content); err != nil {
			return err
		}
		if chunk.Done {
			break
		}
	}
	return nil
}

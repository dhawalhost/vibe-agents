package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dhawalhost/vibe-agents/internal/llm"
)

// mockProvider implements LLMProvider for testing
type mockProvider struct {
	name     string
	response string
	err      error
}

func (m *mockProvider) Name() string    { return m.name }
func (m *mockProvider) Models() []string { return []string{"mock-model"} }

func (m *mockProvider) Generate(_ context.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.LLMResponse{
		Content:  m.response,
		Model:    "mock-model",
		Provider: m.name,
	}, nil
}

func (m *mockProvider) GenerateStream(_ context.Context, req llm.LLMRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		resp, err := m.Generate(context.Background(), req)
		if err != nil {
			ch <- llm.StreamChunk{Error: err}
			return
		}
		ch <- llm.StreamChunk{Content: resp.Content, Done: true}
	}()
	return ch, nil
}

func TestStreamToString(t *testing.T) {
	ch := make(chan llm.StreamChunk, 3)
	ch <- llm.StreamChunk{Content: "Hello"}
	ch <- llm.StreamChunk{Content: " World"}
	ch <- llm.StreamChunk{Content: "!", Done: true}
	close(ch)

	result, err := llm.StreamToString(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello World!" {
		t.Errorf("expected %q, got %q", "Hello World!", result)
	}
}

func TestStreamToWriter(t *testing.T) {
	ch := make(chan llm.StreamChunk, 3)
	ch <- llm.StreamChunk{Content: "Hello"}
	ch <- llm.StreamChunk{Content: " World"}
	ch <- llm.StreamChunk{Done: true}
	close(ch)

	var buf strings.Builder
	err := llm.StreamToWriter(ch, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "Hello World" {
		t.Errorf("expected %q, got %q", "Hello World", buf.String())
	}
}

func TestProviderRouter(t *testing.T) {
	router := llm.NewProviderRouter("mock", "mock-model")
	mock := &mockProvider{name: "mock", response: "test response"}
	router.Register("mock", mock)

	prov, model, err := router.GetProvider("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.Name() != "mock" {
		t.Errorf("expected mock provider, got %s", prov.Name())
	}
	if model != "mock-model" {
		t.Errorf("expected mock-model, got %s", model)
	}
}

func TestProviderRouterWithRoute(t *testing.T) {
	router := llm.NewProviderRouter("default", "default-model")
	mock := &mockProvider{name: "mock"}
	router.Register("mock", mock)
	router.SetRoute("test-agent", &llm.ProviderRoute{
		Provider: "mock",
		Model:    "special-model",
	})

	prov, model, err := router.GetProvider("test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.Name() != "mock" {
		t.Errorf("expected mock provider")
	}
	if model != "special-model" {
		t.Errorf("expected special-model, got %s", model)
	}
}

func TestProviderRouterUnknownProvider(t *testing.T) {
	router := llm.NewProviderRouter("unknown", "model")
	_, _, err := router.GetProvider("")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestProviderRouterGenerate(t *testing.T) {
	router := llm.NewProviderRouter("mock", "mock-model")
	mock := &mockProvider{name: "mock", response: "generated content"}
	router.Register("mock", mock)

	resp, err := router.Generate(context.Background(), "", llm.LLMRequest{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "generated content" {
		t.Errorf("expected generated content, got %q", resp.Content)
	}
}

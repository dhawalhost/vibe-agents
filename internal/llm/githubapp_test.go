package llm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	openai "github.com/sashabaranov/go-openai"
)

// roundTripFunc allows using a plain function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newTestAppsTransport creates a *ghinstallation.AppsTransport backed by a
// freshly generated RSA key so tests don't depend on real credentials.
func newTestAppsTransport(t *testing.T, rt http.RoundTripper) *ghinstallation.AppsTransport {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test RSA key: %v", err)
	}
	return ghinstallation.NewAppsTransportFromPrivateKey(rt, 12345, key)
}

// serveInstallations returns an httptest.Server that responds to
// GET /app/installations with the provided installation IDs.
func serveInstallations(ids []int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type inst struct {
			ID int64 `json:"id"`
		}
		var body []inst
		for _, id := range ids {
			body = append(body, inst{ID: id})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
}

// transportToServer returns an http.RoundTripper that rewrites all requests to
// target the given httptest.Server (preserving path/query).
func transportToServer(srv *httptest.Server) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req2 := req.Clone(req.Context())
		req2.URL.Scheme = "http"
		req2.URL.Host = srv.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req2)
	})
}

func TestDiscoverInstallationID_OneInstallation(t *testing.T) {
	srv := serveInstallations([]int64{42})
	defer srv.Close()

	atr := newTestAppsTransport(t, transportToServer(srv))
	id, err := discoverInstallationID(context.Background(), atr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Fatalf("got id %d, want 42", id)
	}
}

func TestDiscoverInstallationID_NoInstallations(t *testing.T) {
	srv := serveInstallations(nil)
	defer srv.Close()

	atr := newTestAppsTransport(t, transportToServer(srv))
	_, err := discoverInstallationID(context.Background(), atr)
	if err == nil {
		t.Fatal("expected error for zero installations, got nil")
	}
	if !strings.Contains(err.Error(), "no installations") {
		t.Fatalf("error %q should mention no installations", err.Error())
	}
}

func TestDiscoverInstallationID_MultipleInstallations(t *testing.T) {
	srv := serveInstallations([]int64{1, 2, 3})
	defer srv.Close()

	atr := newTestAppsTransport(t, transportToServer(srv))
	_, err := discoverInstallationID(context.Background(), atr)
	if err == nil {
		t.Fatal("expected error for multiple installations, got nil")
	}
	if !strings.Contains(err.Error(), "3 installations") {
		t.Fatalf("error %q should mention the count", err.Error())
	}
}

func TestDiscoverInstallationID_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	atr := newTestAppsTransport(t, transportToServer(srv))
	_, err := discoverInstallationID(context.Background(), atr)
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error %q should contain status code 403", err.Error())
	}
}

func TestDiscoverInstallationID_ContextCancellation(t *testing.T) {
	// Server that blocks until the request context is cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	atr := newTestAppsTransport(t, transportToServer(srv))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before the request is made

	_, err := discoverInstallationID(ctx, atr)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestBuildCopilotProvider_TokenPath(t *testing.T) {
	p, err := BuildCopilotProvider("", "", "", "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "copilot" {
		t.Fatalf("got provider name %q, want %q", p.Name(), "copilot")
	}
}

func TestBuildCopilotProvider_NoCredentials(t *testing.T) {
	_, err := BuildCopilotProvider("", "", "", "")
	if err == nil {
		t.Fatal("expected error when no credentials provided, got nil")
	}
}

func TestBuildCopilotProvider_MissingPrivateKey(t *testing.T) {
	// App ID provided but no private key → must fall through to token check.
	// With no token either, should return the config-error message.
	_, err := BuildCopilotProvider("12345", "", "", "")
	if err == nil {
		t.Fatal("expected error when private key is missing, got nil")
	}
}

func TestBuildCopilotProvider_InvalidAppID(t *testing.T) {
	_, err := BuildCopilotProvider("not-a-number", "fake-pem", "", "")
	if err == nil {
		t.Fatal("expected error for invalid App ID, got nil")
	}
	if !strings.Contains(err.Error(), "parse GitHub App ID") {
		t.Fatalf("error %q does not contain expected substring", err.Error())
	}
}

func TestBuildCopilotProvider_InvalidAppIDFallsBackToToken(t *testing.T) {
	p, err := BuildCopilotProvider("not-a-number", "fake-pem", "", "token-fallback")
	if err != nil {
		t.Fatalf("unexpected error with token fallback: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildCopilotProvider_PrefersTokenWhenBothProvided(t *testing.T) {
	// Invalid App credentials should be ignored when a static OAuth token exists.
	p, err := BuildCopilotProvider("not-a-number", "fake-pem", "", "token-preferred")
	if err != nil {
		t.Fatalf("unexpected error when token is provided: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

type staticErrorTokenSource struct {
	err error
}

func (s *staticErrorTokenSource) Token(context.Context) (string, error) {
	return "", s.err
}

func TestFailoverTokenSource_UsesSecondaryWhenPrimaryFails(t *testing.T) {
	fts := &failoverTokenSource{
		primary:   &staticErrorTokenSource{err: errors.New("boom")},
		secondary: NewStaticTokenSource("secondary-token"),
	}

	tok, err := fts.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "secondary-token" {
		t.Fatalf("got %q, want %q", tok, "secondary-token")
	}
}

func TestNormalizeCopilotModel(t *testing.T) {
	if got := normalizeCopilotModel("gpt-4o"); got != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("got %q, want %q", got, "gpt-4o-mini-2024-07-18")
	}
	if got := normalizeCopilotModel(""); got != "auto" {
		t.Fatalf("got %q, want %q", got, "auto")
	}
	if got := normalizeCopilotModel("auto"); got != "auto" {
		t.Fatalf("got %q, want %q", got, "auto")
	}
	if got := normalizeCopilotModel("gpt-4o-mini"); got != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("got %q, want %q", got, "gpt-4o-mini-2024-07-18")
	}
	if got := normalizeCopilotModel("claude-sonnet-4"); got != "claude-sonnet-4" {
		t.Fatalf("got %q, want unchanged model", got)
	}
}

func TestResolveModel_AutoRemembersSuccessfulModel(t *testing.T) {
	p := &CopilotProvider{}
	if got := p.resolveModel(context.Background(), "auto"); got != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("got %q, want %q", got, "gpt-4o-mini-2024-07-18")
	}
	p.rememberWorkingModel("gpt-4.1")
	if got := p.resolveModel(context.Background(), "auto"); got != "gpt-4.1" {
		t.Fatalf("got %q, want remembered model %q", got, "gpt-4.1")
	}
}

func TestApplyCopilotTokenLimit(t *testing.T) {
	req := openai.ChatCompletionRequest{Model: "gpt-5-mini"}
	applyCopilotTokenLimit(&req, 123)
	if req.MaxCompletionTokens != 123 {
		t.Fatalf("got max_completion_tokens %d, want %d", req.MaxCompletionTokens, 123)
	}
	if req.MaxTokens != 0 {
		t.Fatalf("got max_tokens %d, want 0", req.MaxTokens)
	}

	req = openai.ChatCompletionRequest{Model: "gpt-4o-mini-2024-07-18"}
	applyCopilotTokenLimit(&req, 77)
	if req.MaxTokens != 77 {
		t.Fatalf("got max_tokens %d, want %d", req.MaxTokens, 77)
	}
}

func TestApplyCopilotTemperature(t *testing.T) {
	req := openai.ChatCompletionRequest{Model: "gpt-5-mini"}
	applyCopilotTemperature(&req, 0.3)
	if req.Temperature != 0 {
		t.Fatalf("got temperature %v, want 0 for gpt-5-mini", req.Temperature)
	}

	req = openai.ChatCompletionRequest{Model: "gpt-4.1"}
	applyCopilotTemperature(&req, 0.3)
	if req.Temperature != float32(0.3) {
		t.Fatalf("got temperature %v, want 0.3", req.Temperature)
	}
}

func TestIsRetryableCopilotModelError_422UnprocessableEntity(t *testing.T) {
	err := errors.New("error, status code: 422, status: 422 Unprocessable Entity, message: invalid character 'U' looking for beginning of value, body: Unprocessable Entity")
	if !isRetryableCopilotModelError(err) {
		t.Fatal("expected plain-text 422 Copilot response to remain eligible for fallback")
	}
}

func TestFallbackModels_SkipsGPT5MiniForChatFallbacks(t *testing.T) {
	p := &CopilotProvider{}
	models := p.fallbackModels(context.Background(), "gpt-4o-mini-2024-07-18")
	for _, model := range models {
		if model == "gpt-5-mini" {
			t.Fatalf("unexpected gpt-5-mini in fallback list: %v", models)
		}
	}
}

func TestCopilotTransport_SetsOpenAIIntentHeader(t *testing.T) {
	var captured *http.Request
	tr := &copilotTransport{
		tokenSource: NewStaticTokenSource("test-token"),
		base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			captured = r.Clone(r.Context())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    r,
			}, nil
		}),
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.githubcopilot.com/chat/completions", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if captured == nil {
		t.Fatal("expected request to be captured")
	}
	if got := captured.Header.Get("OpenAI-Intent"); got != "conversation-panel" {
		t.Fatalf("got OpenAI-Intent %q, want %q", got, "conversation-panel")
	}
}

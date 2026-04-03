package llm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
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

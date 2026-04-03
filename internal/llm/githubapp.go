package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
)

const (
	// discoveryTimeout is the context deadline used during installation
	// ID auto-discovery.
	discoveryTimeout = 30 * time.Second
)

// TokenSource is the interface for anything that can supply a bearer token.
// The caller's context is passed so that token refresh respects cancellation
// and deadlines from the calling code (e.g. an incoming HTTP request).
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource returns a fixed token (used for GITHUB_TOKEN / OAuth tokens).
type StaticTokenSource struct {
	token string
}

// NewStaticTokenSource creates a StaticTokenSource from a pre-existing token string.
func NewStaticTokenSource(token string) *StaticTokenSource {
	return &StaticTokenSource{token: token}
}

func (s *StaticTokenSource) Token(_ context.Context) (string, error) {
	return s.token, nil
}

// GitHubAppTokenSource obtains short-lived GitHub App installation access tokens
// using the ghinstallation SDK. Tokens are cached and refreshed automatically.
type GitHubAppTokenSource struct {
	transport *ghinstallation.Transport
}

// NewGitHubAppTokenSource creates a GitHubAppTokenSource.
// privateKeyPEM is the RSA private key in PEM format (PKCS#1 or PKCS#8).
// Literal "\n" sequences in the string are converted to real newlines so the
// value can be stored as a single-line environment variable.
// If installationID is empty the installation is discovered automatically via
// the GitHub API. Auto-discovery succeeds only when the App has exactly one
// installation; set GITHUB_APP_INSTALLATION_ID explicitly when the App is
// installed on multiple accounts or organisations.
func NewGitHubAppTokenSource(appID, privateKeyPEM, installationID string) (*GitHubAppTokenSource, error) {
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, `\n`, "\n")

	appIDInt, err := strconv.ParseInt(appID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App ID %q: %w", appID, err)
	}

	atr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appIDInt, []byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("create GitHub App transport: %w", err)
	}

	var instID int64
	if installationID != "" {
		instID, err = strconv.ParseInt(installationID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse installation ID %q: %w", installationID, err)
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel()
		instID, err = discoverInstallationID(ctx, atr)
		if err != nil {
			return nil, fmt.Errorf("auto-discover GitHub App installation ID: %w", err)
		}
	}

	tr := ghinstallation.NewFromAppsTransport(atr, instID)
	return &GitHubAppTokenSource{transport: tr}, nil
}

// Token returns a valid installation access token, refreshing automatically when
// near expiry. The provided context is propagated to the underlying network call
// so that cancellation and deadlines from the caller are respected.
func (g *GitHubAppTokenSource) Token(ctx context.Context) (string, error) {
	return g.transport.Token(ctx)
}

// discoverInstallationID fetches the list of installations for the GitHub App
// and returns the installation ID. It errors if the App has zero installations
// (not installed anywhere) or more than one installation (ambiguous — the caller
// must set GITHUB_APP_INSTALLATION_ID explicitly in that case).
func discoverInstallationID(ctx context.Context, atr *ghinstallation.AppsTransport) (int64, error) {
	client := &http.Client{Transport: atr}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/app/installations", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("list installations: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read installations response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var installations []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &installations); err != nil {
		return 0, fmt.Errorf("parse installations response: %w", err)
	}
	switch len(installations) {
	case 0:
		return 0, fmt.Errorf("GitHub App has no installations; install it on your account or organisation first, " +
			"or set GITHUB_APP_INSTALLATION_ID explicitly")
	case 1:
		return installations[0].ID, nil
	default:
		return 0, fmt.Errorf("GitHub App has %d installations; set GITHUB_APP_INSTALLATION_ID explicitly to select one", len(installations))
	}
}

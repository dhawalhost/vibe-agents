package llm

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// jwtClockSkewSeconds is the number of seconds the JWT issue time is
	// back-dated to absorb clock skew between the local machine and GitHub.
	jwtClockSkewSeconds = 60

	// jwtValidityMinutes is the lifetime of the generated JWT.
	jwtValidityMinutes = 9

	// tokenRefreshBuffer is how early before expiry a cached token is refreshed.
	tokenRefreshBuffer = 5 * time.Minute

	// githubAPITimeout is the HTTP timeout for GitHub API calls.
	githubAPITimeout = 30 * time.Second
)
type TokenSource interface {
	Token() (string, error)
}

// StaticTokenSource returns a fixed token (used for GITHUB_TOKEN / OAuth tokens).
type StaticTokenSource struct {
	token string
}

// NewStaticTokenSource creates a StaticTokenSource from a pre-existing token string.
func NewStaticTokenSource(token string) *StaticTokenSource {
	return &StaticTokenSource{token: token}
}

func (s *StaticTokenSource) Token() (string, error) {
	return s.token, nil
}

// GitHubAppTokenSource obtains short-lived GitHub App installation access tokens.
// It generates a signed JWT from the App's private key, exchanges it with the
// GitHub API for an installation access token, and caches that token until it
// is about to expire.
type GitHubAppTokenSource struct {
	appID          string
	privateKey     *rsa.PrivateKey
	installationID string
	httpClient     *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewGitHubAppTokenSource creates a GitHubAppTokenSource.
// privateKeyPEM is the RSA private key in PEM format (PKCS#1 or PKCS#8).
// Literal "\n" sequences in the string are converted to real newlines so the
// value can be stored in a single-line environment variable.
func NewGitHubAppTokenSource(appID, privateKeyPEM, installationID string) (*GitHubAppTokenSource, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	return &GitHubAppTokenSource{
		appID:          appID,
		privateKey:     key,
		installationID: installationID,
		httpClient:     &http.Client{Timeout: githubAPITimeout},
	}, nil
}

// Token returns a valid installation access token, refreshing it when it is
// within 5 minutes of expiry.
func (g *GitHubAppTokenSource) Token() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cachedToken != "" && time.Now().Add(tokenRefreshBuffer).Before(g.tokenExpiry) {
		return g.cachedToken, nil
	}
	return g.refresh()
}

// refresh obtains a new installation access token (caller must hold g.mu).
func (g *GitHubAppTokenSource) refresh() (string, error) {
	jwt, err := g.generateJWT()
	if err != nil {
		return "", fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", g.installationID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request installation token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("GitHub API returned empty token")
	}

	g.cachedToken = result.Token
	g.tokenExpiry = result.ExpiresAt
	return g.cachedToken, nil
}

// generateJWT creates a signed RS256 JWT for authenticating as the GitHub App.
func (g *GitHubAppTokenSource) generateJWT() (string, error) {
	now := time.Now()

	header := jwtBase64(mustMarshalJSON(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}))
	payload := jwtBase64(mustMarshalJSON(map[string]interface{}{
		"iat": now.Unix() - jwtClockSkewSeconds,
		"exp": now.Add(jwtValidityMinutes * time.Minute).Unix(),
		"iss": g.appID,
	}))

	sigInput := header + "." + payload
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, g.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return sigInput + "." + jwtBase64(sig), nil
}

// jwtBase64 encodes data using base64url without padding (RFC 7515).
func jwtBase64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// mustMarshalJSON marshals v to JSON and panics on error (only used with
// hard-coded map literals that cannot fail).
func mustMarshalJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key (PKCS#1 or PKCS#8).
// Literal `\n` sequences are replaced with real newlines for env-var convenience.
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	pemStr = strings.ReplaceAll(pemStr, `\n`, "\n")

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 private key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

package serverclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// Client is the shared transport core for all serverclient mode adapters.
// One Client per process; mode adapters borrow it and add their own paths
// + payload encoding. Safe for concurrent use.
type Client struct {
	baseURL      *url.URL
	bearerToken  string
	httpClient   *http.Client
	userAgent    string
	debug        bool
	timeoutOnReq time.Duration
}

// Options configures a Client. Use NewFromConfig to wire from config.toml
// or construct directly for tests.
type Options struct {
	// BaseURL is the root of the speechkit-server, e.g.
	// "https://speechkit.example.com" or "http://localhost:8080".
	BaseURL string

	// BearerToken is sent verbatim as the Authorization header value
	// "Bearer <token>". Leave empty for endpoints that don't require auth
	// (only /healthz and /readyz qualify on the server today).
	BearerToken string

	// HTTPClient overrides the default http.Client. Useful in tests with
	// httptest.Server. If nil, a sensible default with the timeout below
	// is constructed.
	HTTPClient *http.Client

	// RequestTimeout caps each non-streaming request. WS upgrades for the
	// Voice Agent ignore this and use the request context's deadline. 0
	// means no client-side timeout (server-side limits still apply).
	RequestTimeout time.Duration

	// UserAgent is sent in the User-Agent header. Defaults to
	// "speechkit-serverclient/<version>".
	UserAgent string

	// Debug, when true, logs request lines + response status to stderr.
	// Tests opt in via Options{Debug: testing.Verbose()}.
	Debug bool
}

const defaultUserAgent = "speechkit-serverclient/0.26"

// New constructs a Client from explicit Options. baseURL must be parseable
// as an absolute URL with http or https scheme.
func New(opts Options) (*Client, error) {
	trimmed := strings.TrimSpace(opts.BaseURL)
	if trimmed == "" {
		return nil, errors.New("serverclient: BaseURL is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("serverclient: invalid BaseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("serverclient: BaseURL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("serverclient: BaseURL has no host")
	}
	// Strip any trailing slash so subsequent path joins are predictable.
	u.Path = strings.TrimRight(u.Path, "/")

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: opts.RequestTimeout}
	}

	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	return &Client{
		baseURL:      u,
		bearerToken:  strings.TrimSpace(opts.BearerToken),
		httpClient:   httpClient,
		userAgent:    ua,
		debug:        opts.Debug,
		timeoutOnReq: opts.RequestTimeout,
	}, nil
}

// NewFromConfig resolves the bearer token from the env var named in
// cfg.BearerTokenEnv and constructs a Client. Returns an error if the env
// var is not set or empty when cfg.Enabled is true. When cfg.Enabled is
// false the function returns (nil, ErrServerConnectionDisabled) so callers
// can branch cleanly.
func NewFromConfig(cfg config.ServerConnectionConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, ErrServerConnectionDisabled
	}
	baseURL := strings.TrimSpace(cfg.URL)
	if baseURL == "" {
		return nil, errors.New("serverclient: server_connection.url is required when enabled")
	}
	envName := strings.TrimSpace(cfg.BearerTokenEnv)
	if envName == "" {
		envName = "SPEECHKIT_SERVER_TOKEN"
	}
	token := strings.TrimSpace(config.ResolveSecret(envName))
	if token == "" {
		return nil, fmt.Errorf("serverclient: env var %q is empty; cannot authenticate", envName)
	}
	timeout := time.Duration(cfg.RequestTimeoutSec) * time.Second
	return New(Options{
		BaseURL:        baseURL,
		BearerToken:    token,
		RequestTimeout: timeout,
	})
}

// ErrServerConnectionDisabled signals that NewFromConfig was called with
// cfg.Enabled = false. Callers typically branch on this and fall back to
// the in-process kernel.
var ErrServerConnectionDisabled = errors.New("serverclient: server_connection disabled")

// BaseURL returns the configured base URL. Useful for the Voice Agent
// adapter when it needs to derive WS URLs from the same origin.
func (c *Client) BaseURL() *url.URL {
	if c == nil || c.baseURL == nil {
		return nil
	}
	clone := *c.baseURL
	return &clone
}

// HTTPClient returns the underlying *http.Client. Mode adapters use it
// directly so they can stream uploads/downloads without buffering.
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c.httpClient
}

// BearerToken returns the configured bearer token. The Voice Agent adapter
// needs it to attach the same token to the initial POST /sessions and to
// pass through to the WebSocket query-string ticket logic.
func (c *Client) BearerToken() string {
	if c == nil {
		return ""
	}
	return c.bearerToken
}

// newRequest is the central place that attaches Authorization, User-Agent,
// and Content-Type headers. path must start with "/" â€” it is joined to the
// configured BaseURL preserving any base path the user configured (e.g.
// "https://api.example.com/speechkit").
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Request, error) {
	if c == nil || c.baseURL == nil {
		return nil, errors.New("serverclient: nil Client")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("serverclient: path must start with /, got %q", path)
	}
	u := *c.baseURL
	u.Path += path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("serverclient: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// Health hits /healthz to confirm the server is reachable. Returns nil on
// 200, a wrapped error otherwise. Use it from /readyz dashboards on the
// device-target so a misconfigured ServerConnection surfaces early.
func (c *Client) Health(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/healthz", nil, "")
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("serverclient: GET /healthz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("serverclient: GET /healthz returned %d", resp.StatusCode)
	}
	return nil
}

package serverclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string // substring of the error
	}{
		{name: "empty", baseURL: "", want: "BaseURL is required"},
		{name: "not absolute", baseURL: "speechkit.example.com", want: "scheme must be http or https"},
		{name: "bad scheme", baseURL: "ftp://example.com", want: "scheme must be http or https"},
		{name: "no host", baseURL: "http:///path", want: "no host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Options{BaseURL: tt.baseURL})
			if err == nil {
				t.Fatalf("New(%q) returned nil error, want error containing %q", tt.baseURL, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNewStripsTrailingSlash(t *testing.T) {
	c, err := New(Options{BaseURL: "http://localhost:8080/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.BaseURL().Path; got != "" {
		t.Errorf("BaseURL().Path = %q, want empty", got)
	}
}

func TestNewFromConfigDisabled(t *testing.T) {
	_, err := NewFromConfig(config.ServerConnectionConfig{Enabled: false})
	if !errors.Is(err, ErrServerConnectionDisabled) {
		t.Fatalf("err = %v, want ErrServerConnectionDisabled", err)
	}
}

func TestNewFromConfigRequiresURL(t *testing.T) {
	_, err := NewFromConfig(config.ServerConnectionConfig{Enabled: true, URL: ""})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("err = %v, want url-required error", err)
	}
}

func TestNewFromConfigRequiresEnvVar(t *testing.T) {
	t.Setenv("SC_TEST_TOKEN", "")
	_, err := NewFromConfig(config.ServerConnectionConfig{
		Enabled:        true,
		URL:            "http://localhost:8080",
		BearerTokenEnv: "SC_TEST_TOKEN",
	})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("err = %v, want empty-env error", err)
	}
}

func TestNewFromConfigSuccess(t *testing.T) {
	t.Setenv("SC_TEST_TOKEN_OK", "secret-value")
	c, err := NewFromConfig(config.ServerConnectionConfig{
		Enabled:           true,
		URL:               "http://localhost:8080",
		BearerTokenEnv:    "SC_TEST_TOKEN_OK",
		AuthMode:          config.ServerConnectionAuthModeBearer,
		RequestTimeoutSec: 7,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if c.BearerToken() != "secret-value" {
		t.Errorf("BearerToken = %q, want secret-value", c.BearerToken())
	}
	if c.timeoutOnReq.Seconds() != 7 {
		t.Errorf("timeoutOnReq = %v, want 7s", c.timeoutOnReq)
	}
}

func TestNewFromConfigSupportsAPIKeyAuthMode(t *testing.T) {
	t.Setenv("SC_TEST_CUSTOM_API_KEY", "custom-secret")
	c, err := NewFromConfig(config.ServerConnectionConfig{
		Enabled:        true,
		URL:            "https://speechkit-api.example.com/v1/speechkit",
		BearerTokenEnv: "SC_TEST_CUSTOM_API_KEY",
		AuthMode:       config.ServerConnectionAuthModeAPIKey,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if c.AuthMode() != config.ServerConnectionAuthModeAPIKey {
		t.Fatalf("AuthMode = %q", c.AuthMode())
	}
}

func TestNewRequestAvoidsDuplicateVersionForMountedBase(t *testing.T) {
	c, err := New(Options{
		BaseURL:     "https://speechkit-api.example.com/v1/speechkit",
		BearerToken: "custom-secret",
		AuthMode:    config.ServerConnectionAuthModeAPIKey,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req, err := c.newRequest(context.Background(), http.MethodPost, "/v1/voiceagent/sessions", strings.NewReader("{}"), "application/json")
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if got, want := req.URL.Path, "/v1/speechkit/voiceagent/sessions"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}

func TestNewRequestKeepsVersionForOriginBase(t *testing.T) {
	c, err := New(Options{
		BaseURL:     "https://speechkit.example.com",
		BearerToken: "server-token",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req, err := c.newRequest(context.Background(), http.MethodPost, "/v1/voiceagent/sessions", strings.NewReader("{}"), "application/json")
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	if got, want := req.URL.Path, "/v1/voiceagent/sessions"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}

func TestNewFromConfigDefaultsAPIKeyTokenEnv(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "server-secret")
	c, err := NewFromConfig(config.ServerConnectionConfig{
		Enabled:  true,
		URL:      "https://speechkit-api.example.com/v1/speechkit",
		AuthMode: config.ServerConnectionAuthModeAPIKey,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if c.BearerToken() != "server-secret" {
		t.Fatalf("BearerToken = %q", c.BearerToken())
	}
}

func TestNewFromConfigResolvesNamedSecretToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	t.Setenv("SC_TEST_STORED_TOKEN", "")
	if err := secrets.SetNamedSecret("SC_TEST_STORED_TOKEN", "stored-secret-value"); err != nil {
		t.Fatalf("set named secret: %v", err)
	}

	c, err := NewFromConfig(config.ServerConnectionConfig{
		Enabled:           true,
		URL:               "http://localhost:8080",
		BearerTokenEnv:    "SC_TEST_STORED_TOKEN",
		RequestTimeoutSec: 7,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if c.BearerToken() != "stored-secret-value" {
		t.Errorf("BearerToken = %q, want stored-secret-value", c.BearerToken())
	}
}

func TestHTTPClientNilReceiverReturnsBoundedFallback(t *testing.T) {
	var c *Client
	got := c.HTTPClient()
	if got == nil {
		t.Fatal("HTTPClient() on nil receiver returned nil; want bounded fallback")
	}
	if got == http.DefaultClient {
		t.Fatal("HTTPClient() on nil receiver returned http.DefaultClient (no timeout); want bounded fallback (B2 regression)")
	}
	if got.Timeout <= 0 {
		t.Errorf("HTTPClient() on nil receiver returned client with Timeout=%v; want > 0 (no-timeout would hang the embedder host)", got.Timeout)
	}
}

func TestHTTPClientReturnsConfiguredClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * 1000 * 1000 * 1000} // 7s as time.Duration
	c, err := New(Options{BaseURL: "http://localhost:8080", HTTPClient: custom})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.HTTPClient(); got != custom {
		t.Errorf("HTTPClient() = %p; want injected custom client %p", got, custom)
	}
}

func TestHealthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer abc" {
			t.Errorf("missing/wrong Authorization header: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Options{
		BaseURL:     srv.URL,
		BearerToken: "abc",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestHealthUsesAPIKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "abc" {
			t.Errorf("missing/wrong X-Api-Key header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header should be empty for api_key auth: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Options{
		BaseURL:     srv.URL,
		BearerToken: "abc",
		AuthMode:    config.ServerConnectionAuthModeAPIKey,
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestHealthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Health err = %v, want 503-bearing error", err)
	}
}

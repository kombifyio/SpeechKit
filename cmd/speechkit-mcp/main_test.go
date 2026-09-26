package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAgentInstallPlanUsesSafePreviewDefaults(t *testing.T) {
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"docs": true}}, docs: loadDocs()}
	result, structured, err := app.installPlan(context.Background(), nil, installPlanInput{Channel: "preview"})
	if err != nil {
		t.Fatalf("installPlan: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "--channel preview") {
		t.Fatalf("install plan missing preview channel:\n%s", text)
	}
	if !strings.Contains(text, "127.0.0.1:8080") {
		t.Fatalf("install plan missing safe loopback default:\n%s", text)
	}
	if strings.Contains(text, "latest") && strings.Contains(text, "preview") {
		t.Fatalf("preview install plan should not tell agents to mutate latest:\n%s", text)
	}
	out, ok := structured.(map[string]any)
	if !ok || out["channel"] != "preview" {
		t.Fatalf("structured = %#v, want preview channel", structured)
	}
}

func TestAgentScaffoldIntegrationReturnsStarterFiles(t *testing.T) {
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"docs": true}}, docs: loadDocs()}
	_, structured, err := app.scaffoldIntegration(context.Background(), nil, scaffoldInput{
		Template: "browser-dictation-react",
		Vars: map[string]string{
			"APP_NAME":             "agent-dictation",
			"SPEECHKIT_SERVER_URL": "http://localhost:8080",
		},
	})
	if err != nil {
		t.Fatalf("scaffoldIntegration: %v", err)
	}
	out, ok := structured.(map[string]any)
	if !ok || out["template"] != "browser-dictation-react" {
		t.Fatalf("structured = %#v, want browser-dictation-react template", structured)
	}
	files, ok := out["files"].([]map[string]string)
	if !ok || len(files) == 0 {
		t.Fatalf("files = %#v, want generated files", out["files"])
	}
	var foundPackage bool
	for _, file := range files {
		if file["path"] == "package.json" && strings.Contains(file["content"], `"name": "agent-dictation"`) {
			foundPackage = true
		}
		if file["path"] == "" || strings.Contains(file["path"], "..") {
			t.Fatalf("unsafe generated path: %#v", file)
		}
	}
	if !foundPackage {
		t.Fatalf("generated files missing package.json with app name: %#v", files)
	}
}

func TestValidateRequestUsesOpenAPISchema(t *testing.T) {
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"test": true}}, docs: loadDocs()}
	_, structured, err := app.validateRequest(context.Background(), nil, jsonValidationInput{
		Endpoint: "/v1/tts/synthesize",
		Payload:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("validateRequest: %v", err)
	}
	out, ok := structured.(map[string]any)
	if !ok {
		t.Fatalf("structured = %T, want map", structured)
	}
	if valid, _ := out["valid"].(bool); valid {
		t.Fatalf("valid = true, want false for missing required text")
	}
}

func TestRequireMCPTokenRejectsMissingToken(t *testing.T) {
	nextCalled := false
	handler := requireMCPToken("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if nextCalled {
		t.Fatal("next handler was called without token")
	}
}

func TestRequireMCPTokenAcceptsBearerAndHeaderToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "authorization bearer", header: "Authorization", value: "Bearer secret"},
		{name: "mcp token header", header: "X-SpeechKit-MCP-Token", value: "secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nextCalled := false
			handler := requireMCPToken("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusNoContent)
			}))

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set(tc.header, tc.value)
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", rr.Code)
			}
			if !nextCalled {
				t.Fatal("next handler was not called for valid token")
			}
		})
	}
}

func TestNewHTTPServerUsesHardenedTimeouts(t *testing.T) {
	handler := http.NotFoundHandler()
	server := newHTTPServer("127.0.0.1:0", handler)
	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("Addr = %q, want 127.0.0.1:0", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("server handler was not set")
	}
	if server.ReadHeaderTimeout != 15*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 15s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %s, want 30s", server.ReadTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want 0 for streamable MCP responses", server.WriteTimeout)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Fatalf("IdleTimeout = %s, want 120s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, 1<<20)
	}
}

func TestTranscribeDisablesAudioPathForHTTPTransport(t *testing.T) {
	app := &speechkitMCP{opts: serverOptions{transport: "http"}}
	result, _, err := app.transcribe(context.Background(), nil, transcribeInput{AudioPath: "hello.wav"})
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "audio_path is disabled") {
		t.Fatalf("result = %+v, want audio_path disabled message", result)
	}
}

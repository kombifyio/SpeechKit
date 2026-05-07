package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDocsModeExposesToolsResourcesAndPrompts(t *testing.T) {
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"docs": true}}, docs: loadDocs()}
	serverSession, err := app.newServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	foundTool := false
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools: %v", err)
		}
		if tool.Name == "speechkit_docs_search" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatal("speechkit_docs_search not listed")
	}

	foundResource := false
	for resource, err := range clientSession.Resources(ctx, nil) {
		if err != nil {
			t.Fatalf("resources: %v", err)
		}
		if resource.Name == "docs/mcp/README.md" {
			foundResource = true
		}
	}
	if !foundResource {
		t.Fatal("docs/mcp/README.md resource not listed")
	}

	foundPrompt := false
	for prompt, err := range clientSession.Prompts(ctx, nil) {
		if err != nil {
			t.Fatalf("prompts: %v", err)
		}
		if prompt.Name == "speechkit_go_sdk_integration" {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Fatal("speechkit_go_sdk_integration prompt not listed")
	}
}

func TestDocsModeEmbedsOpenAPIWithoutRepoWorkingDirectory(t *testing.T) {
	tmp := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	docs := loadDocs()
	spec := openAPISpec()
	if !strings.Contains(spec, "/v1/dictation/transcribe") {
		t.Fatalf("embedded OpenAPI spec missing dictation endpoint:\n%s", spec)
	}
	if !strings.Contains(docs["docs/server/openapi.v1.yaml"], "/v1/tts/synthesize") {
		t.Fatalf("embedded docs missing OpenAPI content")
	}
	if !strings.Contains(docs["docs/server/asyncapi.v1.yaml"], "SpeechKit Voice Agent WebSocket API") {
		t.Fatalf("embedded docs missing AsyncAPI content")
	}
	if !strings.Contains(asyncAPISpec(), "SpeechKit Voice Agent WebSocket API") {
		t.Fatalf("embedded AsyncAPI spec helper missing Voice Agent contract")
	}
}

func TestDocsModeEmbedsAgentMarkdownEntrypoints(t *testing.T) {
	docs := loadDocs()

	for _, path := range []string{
		"docs/agent/llms.txt",
		"docs/agent/llms-full.txt",
		"docs/agent/getting-started/technical.md",
		"docs/agent/getting-started/agents.md",
		"docs/agent/install/server.md",
		"docs/agent/mcp/speechkit-mcp.md",
	} {
		body := docs[path]
		if !strings.Contains(body, "SpeechKit") {
			t.Fatalf("%s missing from embedded docs", path)
		}
	}
}

func TestPromptTextUsesAgentReadyStarterPrompts(t *testing.T) {
	serverSetup := promptText("speechkit_server_setup")
	if !strings.Contains(serverSetup, "Hi Codex, go to speechkit.cc and install the SpeechKit Server on this server.") {
		t.Fatalf("server setup prompt missing Codex starter:\n%s", serverSetup)
	}
	if !strings.Contains(serverSetup, "https://speechkit.cc/llms.txt") {
		t.Fatalf("server setup prompt missing public Markdown index:\n%s", serverSetup)
	}

	mcpSetup := promptText("speechkit_http_integration")
	if !strings.Contains(mcpSetup, "https://speechkit.cc/api/openapi.v1.yaml") {
		t.Fatalf("HTTP integration prompt missing OpenAPI link:\n%s", mcpSetup)
	}
}

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

func TestAgentSelfCheckPlanListsRequiredProbes(t *testing.T) {
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"test": true}}, docs: loadDocs()}
	result, structured, err := app.selfCheckPlan(context.Background(), nil, selfCheckInput{ServerURL: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("selfCheckPlan: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{"/healthz", "/readyz", "/v1/config", "/v1/catalog/profiles", "/api/openapi.v1.yaml", "/api/asyncapi.v1.yaml"} {
		if !strings.Contains(text, want) {
			t.Fatalf("self-check plan missing %s:\n%s", want, text)
		}
	}
	out, ok := structured.(map[string]any)
	if !ok || out["server_url"] != "http://localhost:8080" {
		t.Fatalf("structured = %#v, want server_url", structured)
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

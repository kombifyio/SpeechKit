package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/scaffold"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

func main() {
	modeFlag := flag.String("mode", "docs", "comma-separated modes: docs,management,test")
	serverURL := flag.String("server", firstNonEmpty(os.Getenv("SPEECHKIT_SERVER_URL"), "http://localhost:8080"), "SpeechKit server URL")
	token := flag.String("token", "", "SpeechKit bearer token")
	transport := flag.String("transport", "stdio", "transport: stdio or http")
	addr := flag.String("addr", "127.0.0.1:8090", "HTTP listen address when --transport=http")
	mcpToken := flag.String("mcp-token", "", "Bearer token required by the MCP HTTP transport")
	flag.Parse()

	opts := serverOptions{
		modes:     parseModes(*modeFlag),
		serverURL: *serverURL,
		token:     firstNonEmpty(*token, os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN")),
		transport: strings.ToLower(strings.TrimSpace(*transport)),
		mcpToken:  firstNonEmpty(*mcpToken, os.Getenv("SPEECHKIT_MCP_TOKEN")),
	}
	c, err := skclient.New(skclient.Options{BaseURL: opts.serverURL, Token: opts.token, UserAgent: "speechkit-mcp/0.1"})
	if err != nil {
		log.Fatal(err)
	}
	app := &speechkitMCP{opts: opts, client: c, docs: loadDocs()}
	server := app.newServer()

	switch opts.transport {
	case "http":
		if opts.modes["management"] && !isLoopbackListenAddr(*addr) && opts.mcpToken == "" {
			log.Fatal("--mcp-token or SPEECHKIT_MCP_TOKEN is required when management mode is exposed over non-loopback HTTP")
		}
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
			SessionTimeout: 30 * time.Minute,
		})
		var httpHandler http.Handler = handler
		if opts.mcpToken != "" {
			httpHandler = requireMCPToken(opts.mcpToken, httpHandler)
		}
		log.Fatal(newHTTPServer(*addr, httpHandler).ListenAndServe())
	default:
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	}
}

type queryInput struct {
	Query string `json:"query" jsonschema:"search query"`
}

type endpointInput struct {
	Path string `json:"path" jsonschema:"HTTP path such as /v1/dictation/transcribe"`
}

type integrationInput struct {
	Language string `json:"language" jsonschema:"go, python, typescript, or curl"`
	Mode     string `json:"mode" jsonschema:"dictation, assist, or voiceagent"`
}

type idInput struct {
	ID string `json:"id"`
}

type providerListInput struct {
	Mode string `json:"mode,omitempty" jsonschema:"dictation, assist, or voiceagent"`
}

type numericIDInput struct {
	ID int64 `json:"id"`
}

type payloadInput struct {
	ID      string         `json:"id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type transcriptListInput struct {
	Limit int `json:"limit,omitempty"`
}

type vocabularyInput struct {
	Language string           `json:"language,omitempty"`
	Entries  []map[string]any `json:"entries"`
}

type transcribeInput struct {
	AudioPath string `json:"audio_path"`
	Language  string `json:"language,omitempty"`
	Model     string `json:"model,omitempty"`
}

type ttsInput struct {
	Text   string  `json:"text"`
	Voice  string  `json:"voice,omitempty"`
	Locale string  `json:"locale,omitempty"`
	Format string  `json:"format,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
}

type configValidationInput struct {
	TOML string `json:"toml_snippet"`
}

type jsonValidationInput struct {
	Endpoint    string          `json:"endpoint"`
	Method      string          `json:"method,omitempty"`
	StatusCode  int             `json:"status_code,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

type codeInput struct {
	ClientCode string `json:"client_code"`
}

type changesInput struct {
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
}

type installPlanInput struct {
	Channel    string `json:"channel,omitempty" jsonschema:"stable or preview"`
	InstallDir string `json:"install_dir,omitempty" jsonschema:"default /opt/speechkit"`
	PublicBind bool   `json:"public_bind,omitempty" jsonschema:"whether to expose port 8080 on all interfaces"`
}

type selfCheckInput struct {
	ServerURL string `json:"server_url,omitempty" jsonschema:"base URL of the SpeechKit Server"`
}

type scaffoldInput struct {
	Template string            `json:"template,omitempty" jsonschema:"template name, default browser-dictation-react"`
	Vars     map[string]string `json:"vars,omitempty" jsonschema:"template variables such as APP_NAME and SPEECHKIT_SERVER_URL"`
}

func (a *speechkitMCP) docsSearch(ctx context.Context, req *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, any, error) {
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" {
		return textResult("query is required"), nil, nil
	}
	type hit struct {
		Path    string `json:"path"`
		Snippet string `json:"snippet"`
	}
	var hits []hit
	for path, body := range a.docs {
		lower := strings.ToLower(body)
		if idx := strings.Index(lower, query); idx >= 0 {
			start := max(0, idx-120)
			end := min(len(body), idx+240)
			hits = append(hits, hit{Path: path, Snippet: strings.TrimSpace(body[start:end])})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Path < hits[j].Path })
	return jsonResult(hits), hits, nil
}

func (a *speechkitMCP) apiEndpoint(ctx context.Context, req *mcp.CallToolRequest, in endpointInput) (*mcp.CallToolResult, any, error) {
	spec := openAPISpec()
	snippet := endpointSnippet(spec, in.Path)
	if snippet == "" {
		return textResult("endpoint not found in OpenAPI spec"), nil, nil
	}
	return textResult(snippet), map[string]string{"path": in.Path, "snippet": snippet}, nil
}

func (a *speechkitMCP) apiOverview(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	endpoints := openAPIEndpoints(openAPISpec())
	return jsonResult(endpoints), endpoints, nil
}

func (a *speechkitMCP) getOpenAPISpec(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return textResult(openAPISpec()), nil, nil
}

func (a *speechkitMCP) getAsyncAPISpec(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return textResult(asyncAPISpec()), nil, nil
}

func (a *speechkitMCP) integrationExample(ctx context.Context, req *mcp.CallToolRequest, in integrationInput) (*mcp.CallToolResult, any, error) {
	language := strings.ToLower(strings.TrimSpace(in.Language))
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "dictation"
	}
	key := "docs/mcp/examples/" + language + "/" + mode + ".md"
	if body, ok := a.docs[key]; ok {
		return textResult(body), map[string]string{"language": in.Language, "mode": in.Mode, "example": body}, nil
	}
	return textResult("No embedded example for language " + in.Language), nil, nil
}

func (a *speechkitMCP) architectureOverview(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return textResult(a.docs["docs/speechkit-architecture-v2.md"] + "\n\n" + a.docs["docs/mcp/README.md"]), nil, nil
}

func (a *speechkitMCP) scaffoldTemplates(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	templates, err := scaffold.ListTemplates()
	if err != nil {
		return nil, nil, err
	}
	out := map[string]any{"templates": templates}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) scaffoldIntegration(ctx context.Context, req *mcp.CallToolRequest, in scaffoldInput) (*mcp.CallToolResult, any, error) {
	template := firstNonEmpty(in.Template, "browser-dictation-react")
	vars := map[string]string{"APP_NAME": "speechkit-agent-demo"}
	for key, value := range in.Vars {
		vars[key] = value
	}
	result, err := scaffold.ScaffoldContext(ctx, scaffold.ScaffoldOptions{
		Template: template,
		Vars:     vars,
	})
	if err != nil {
		return nil, nil, err
	}
	files := make([]map[string]string, 0, len(result.Files))
	for _, file := range result.Files {
		files = append(files, map[string]string{
			"path":    file.RelPath,
			"content": string(file.Content),
		})
	}
	out := map[string]any{
		"template": result.Template,
		"vars":     result.Vars,
		"files":    files,
		"notes": []string{
			"This tool is read-only and does not write files to the host.",
			"Review generated files before applying them to a repository.",
		},
	}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) installPlan(ctx context.Context, req *mcp.CallToolRequest, in installPlanInput) (*mcp.CallToolResult, any, error) {
	channel := strings.ToLower(strings.TrimSpace(in.Channel))
	if channel != "preview" {
		channel = "stable"
	}
	installDir := firstNonEmpty(in.InstallDir, "/opt/speechkit")
	args := []string{}
	if channel == "preview" {
		args = append(args, "--channel", "preview")
	}
	if installDir != "/opt/speechkit" {
		args = append(args, "--dir", shellQuote(installDir))
	}
	if in.PublicBind {
		args = append(args, "--public-bind")
	}
	command := "curl -fsSL https://speechkit.cc/install-server.sh | sh"
	if len(args) > 0 {
		command = "curl -fsSL https://speechkit.cc/install-server.sh | sh -s -- " + strings.Join(args, " ")
	}

	steps := []string{
		"Fetch https://speechkit.cc/llms.txt and https://speechkit.cc/install/server.md.",
		"Run: " + command,
		"Keep the default 127.0.0.1:8080 bind unless the user explicitly requests public access.",
		"Read the generated bearer token from " + installDir + "/.env without printing it into logs.",
		"Verify docker compose ps, http://localhost:8080/healthz, and http://localhost:8080/readyz.",
		"Use https://speechkit.cc/api/openapi.v1.yaml before writing integration code.",
		"Use https://speechkit.cc/api/asyncapi.v1.yaml before implementing Voice Agent WebSocket clients.",
	}
	if in.PublicBind {
		steps = append(steps, "When public_bind is true, place SpeechKit behind TLS and keep bearer auth enabled.")
	}
	if channel == "preview" {
		steps = append(steps, "For preview installs, do not create release tags or retag stable release images.")
	}

	out := map[string]any{
		"channel":     channel,
		"install_dir": installDir,
		"public_bind": in.PublicBind,
		"command":     command,
		"steps":       steps,
	}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) status(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	status, err := a.client.Status(ctx)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(status), status, nil
}

func (a *speechkitMCP) configGet(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	cfg, err := a.client.Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(cfg), cfg, nil
}

func (a *speechkitMCP) providerList(ctx context.Context, req *mcp.CallToolRequest, in providerListInput) (*mcp.CallToolResult, any, error) {
	profiles, err := a.client.CatalogProfiles(ctx, in.Mode)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(profiles), profiles, nil
}

func (a *speechkitMCP) providerReadiness(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	ready, err := a.client.ProviderReadiness(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(ready), ready, nil
}

func (a *speechkitMCP) personasList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Personas(ctx)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Persona(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreatePersona(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdatePersona(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeletePersona(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return textResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) rolesList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Roles(ctx)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Role(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreateRole(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdateRole(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeleteRole(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return textResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) sequencesList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Sequences(ctx)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Sequence(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreateSequence(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdateSequence(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeleteSequence(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return textResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) transcriptsList(ctx context.Context, req *mcp.CallToolRequest, in transcriptListInput) (*mcp.CallToolResult, any, error) {
	list, err := a.client.Transcripts(ctx, in.Limit)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(map[string]any{"transcripts": list}), map[string]any{"transcripts": list}, nil
}

func (a *speechkitMCP) transcriptGet(ctx context.Context, req *mcp.CallToolRequest, in numericIDInput) (*mcp.CallToolResult, any, error) {
	item, err := a.client.Transcript(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(item), item, nil
}

func (a *speechkitMCP) voiceAgentSessionSummary(ctx context.Context, req *mcp.CallToolRequest, in numericIDInput) (*mcp.CallToolResult, any, error) {
	summary, err := a.client.VoiceAgentSessionSummary(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(summary), summary, nil
}

func (a *speechkitMCP) vocabularyGet(ctx context.Context, req *mcp.CallToolRequest, in vocabularyInput) (*mcp.CallToolResult, any, error) {
	entries, err := a.client.VocabularyEntries(ctx, in.Language)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(map[string]any{"entries": entries}), map[string]any{"entries": entries}, nil
}

func (a *speechkitMCP) vocabularyReplace(ctx context.Context, req *mcp.CallToolRequest, in vocabularyInput) (*mcp.CallToolResult, any, error) {
	entries := make([]skclient.DictionaryEntry, 0, len(in.Entries))
	for _, raw := range in.Entries {
		entries = append(entries, skclient.DictionaryEntry{
			Spoken:    stringMapValue(raw, "spoken"),
			Canonical: stringMapValue(raw, "canonical"),
			Language:  firstNonEmpty(stringMapValue(raw, "language"), in.Language),
			Source:    stringMapValue(raw, "source"),
			Enabled:   true,
		})
	}
	out, err := a.client.ReplaceVocabularyEntries(ctx, in.Language, entries)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(map[string]any{"entries": out}), map[string]any{"entries": out}, nil
}

func (a *speechkitMCP) transcribe(ctx context.Context, req *mcp.CallToolRequest, in transcribeInput) (*mcp.CallToolResult, any, error) {
	if a.opts.transport == "http" {
		return textResult("audio_path is disabled for HTTP MCP transport; use local stdio MCP for filesystem audio files."), nil, nil
	}
	result, err := a.client.TranscribeFile(ctx, in.AudioPath, skclient.TranscribeOptions{Language: in.Language, Model: in.Model})
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(result), result, nil
}

func (a *speechkitMCP) ttsSynthesize(ctx context.Context, req *mcp.CallToolRequest, in ttsInput) (*mcp.CallToolResult, any, error) {
	result, err := a.client.TTSSynthesize(ctx, skclient.TTSSynthesizeRequest{
		Text:   in.Text,
		Voice:  in.Voice,
		Locale: in.Locale,
		Format: in.Format,
		Speed:  in.Speed,
	})
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(result), result, nil
}

func (a *speechkitMCP) validateConfig(ctx context.Context, req *mcp.CallToolRequest, in configValidationInput) (*mcp.CallToolResult, any, error) {
	var cfg config.Config
	_, err := toml.Decode(in.TOML, &cfg)
	out := map[string]any{"valid": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) validateRequest(ctx context.Context, req *mcp.CallToolRequest, in jsonValidationInput) (*mcp.CallToolResult, any, error) {
	return validateOpenAPIPayload(ctx, "request", in)
}

func (a *speechkitMCP) validateResponse(ctx context.Context, req *mcp.CallToolRequest, in jsonValidationInput) (*mcp.CallToolResult, any, error) {
	return validateOpenAPIPayload(ctx, "response", in)
}

func (a *speechkitMCP) checkCompatibility(ctx context.Context, req *mcp.CallToolRequest, in codeInput) (*mcp.CallToolResult, any, error) {
	endpoints := openAPIEndpoints(openAPISpec())
	var found []string
	for _, endpoint := range endpoints {
		if strings.Contains(in.ClientCode, endpoint) {
			found = append(found, endpoint)
		}
	}
	out := map[string]any{"known_endpoints_found": found, "valid": len(found) > 0}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) selfCheckPlan(ctx context.Context, req *mcp.CallToolRequest, in selfCheckInput) (*mcp.CallToolResult, any, error) {
	serverURL := strings.TrimRight(firstNonEmpty(in.ServerURL, a.opts.serverURL, "http://localhost:8080"), "/")
	probes := []map[string]string{
		{"name": "compose", "command": "docker compose ps"},
		{"name": "health", "command": "curl -fsS " + serverURL + "/healthz"},
		{"name": "readiness", "command": "curl -fsS " + serverURL + "/readyz"},
		{"name": "config", "command": "curl -fsS -H 'Authorization: Bearer $SPEECHKIT_TOKEN' " + serverURL + "/v1/config"},
		{"name": "catalog", "command": "curl -fsS -H 'Authorization: Bearer $SPEECHKIT_TOKEN' " + serverURL + "/v1/catalog/profiles"},
		{"name": "openapi", "command": "fetch https://speechkit.cc/api/openapi.v1.yaml"},
		{"name": "asyncapi", "command": "fetch https://speechkit.cc/api/asyncapi.v1.yaml"},
	}
	out := map[string]any{
		"server_url": serverURL,
		"probes":     probes,
		"notes": []string{
			"Do not switch auth_mode to none on public hosts.",
			"Treat /readyz degradation as provider or credential work, not as API-contract failure.",
			"Validate request and response JSON with speechkit_validate_request and speechkit_validate_response before writing integration code.",
			"Use the AsyncAPI contract before implementing Voice Agent WebSocket clients.",
		},
	}
	return jsonResult(out), out, nil
}

func (a *speechkitMCP) breakingChanges(ctx context.Context, req *mcp.CallToolRequest, in changesInput) (*mcp.CallToolResult, any, error) {
	raw, readErr := os.ReadFile("CHANGELOG.md")
	if readErr == nil {
		text := string(raw)
		if len(text) > 8000 {
			text = text[:8000]
		}
		return textResult(text), map[string]string{"from": in.FromVersion, "to": in.ToVersion}, nil
	}
	return textResult("No CHANGELOG.md found in current working directory."), nil, nil
}

func validateOpenAPIPayload(ctx context.Context, kind string, in jsonValidationInput) (*mcp.CallToolResult, any, error) {
	endpoint := strings.TrimSpace(in.Endpoint)
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	contentType := firstNonEmpty(in.ContentType, "application/json")
	statusCode := in.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	out := map[string]any{"endpoint": endpoint, "kind": kind, "valid": true}
	if endpoint == "" {
		out["valid"] = false
		out["error"] = "endpoint is required"
		return jsonResult(out), out, nil
	}
	if len(bytes.TrimSpace(in.Payload)) > 0 && !json.Valid(in.Payload) {
		out["valid"] = false
		out["error"] = "payload is not valid JSON"
		return jsonResult(out), out, nil
	}
	doc, err := loadOpenAPIDocument(ctx)
	if err != nil {
		return nil, nil, err
	}
	if method == "" {
		method = inferMethod(doc, endpoint, kind)
	}
	if method == "" {
		out["valid"] = false
		out["error"] = "unknown endpoint or method"
		return jsonResult(out), out, nil
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		return nil, nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(in.Payload))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	route, pathParams, err := router.FindRoute(httpReq)
	if err != nil {
		out["valid"] = false
		out["error"] = err.Error()
		return jsonResult(out), out, nil //nolint:nilerr // A schema mismatch is returned as tool data, not an MCP transport failure.
	}
	options := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	switch kind {
	case "response":
		header := http.Header{"Content-Type": []string{contentType}}
		body := io.NopCloser(bytes.NewReader(in.Payload))
		err = openapi3filter.ValidateResponse(ctx, &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{Request: httpReq, PathParams: pathParams, Route: route, Options: options},
			Status:                 statusCode,
			Header:                 header,
			Body:                   body,
		})
	default:
		err = openapi3filter.ValidateRequest(ctx, &openapi3filter.RequestValidationInput{
			Request:    httpReq,
			PathParams: pathParams,
			Route:      route,
			Options:    options,
		})
	}
	if err != nil {
		out["valid"] = false
		out["error"] = err.Error()
	}
	out["method"] = method
	out["content_type"] = contentType
	if kind == "response" {
		out["status_code"] = statusCode
	}
	return jsonResult(out), out, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func jsonResult(value any) *mcp.CallToolResult {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return textResult(string(raw))
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

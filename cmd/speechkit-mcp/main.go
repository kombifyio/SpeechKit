package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	speechkitdocs "github.com/kombifyio/SpeechKit/docs"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/scaffold"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

type serverOptions struct {
	modes     map[string]bool
	serverURL string
	token     string
	transport string
	mcpToken  string
}

type speechkitMCP struct {
	opts   serverOptions
	client *skclient.Client
	docs   map[string]string
}

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

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Streamable MCP responses can be long-lived; request-side timeouts
		// and idle timeout still bound slowloris and inactive connections.
		WriteTimeout:   0,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
}

func (a *speechkitMCP) newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "speechkit-mcp", Version: "0.1.0"}, nil)
	if a.opts.modes["docs"] {
		a.addDocs(server)
	}
	if a.opts.modes["management"] {
		a.addManagement(server)
	}
	if a.opts.modes["test"] {
		a.addTest(server)
	}
	return server
}

func (a *speechkitMCP) addDocs(server *mcp.Server) {
	mcp.AddTool(server, mcpTool("speechkit_docs_search", "Search SpeechKit docs, OpenAPI, and AsyncAPI snippets.", true, false, true), a.docsSearch)
	mcp.AddTool(server, mcpTool("speechkit_api_endpoint", "Return OpenAPI details for one endpoint path.", true, false, true), a.apiEndpoint)
	mcp.AddTool(server, mcpTool("speechkit_api_overview", "List SpeechKit API endpoints grouped from OpenAPI.", true, false, true), a.apiOverview)
	mcp.AddTool(server, mcpTool("speechkit_get_openapi_spec", "Return the SpeechKit OpenAPI YAML.", true, false, true), a.getOpenAPISpec)
	mcp.AddTool(server, mcpTool("speechkit_get_asyncapi_spec", "Return the SpeechKit Voice Agent AsyncAPI YAML.", true, false, true), a.getAsyncAPISpec)
	mcp.AddTool(server, mcpTool("speechkit_integration_example", "Return an integration snippet by language and mode.", true, false, true), a.integrationExample)
	mcp.AddTool(server, mcpTool("speechkit_architecture_overview", "Summarize SpeechKit modes and API-first architecture.", true, false, true), a.architectureOverview)
	mcp.AddTool(server, mcpTool("speechkit_install_plan", "Return a safe, read-only server install plan for agents.", true, false, true), a.installPlan)
	mcp.AddTool(server, mcpTool("speechkit_scaffold_templates", "List read-only SpeechKit starter integration templates.", true, false, true), a.scaffoldTemplates)
	mcp.AddTool(server, mcpTool("speechkit_scaffold_integration", "Render a starter integration template in memory for an agent to apply.", true, false, true), a.scaffoldIntegration)

	for uri, body := range a.docs {
		uri := uri
		body := body
		server.AddResource(&mcp.Resource{
			Name:        uri,
			Title:       resourceTitle(uri),
			URI:         "speechkit://" + uri,
			MIMEType:    mimeTypeForResource(uri),
			Size:        int64(len(body)),
			Description: "Embedded SpeechKit API-first documentation",
			Annotations: &mcp.Annotations{Audience: []mcp.Role{mcp.Role("assistant")}, Priority: resourcePriority(uri)},
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: mimeTypeForResource(uri), Text: body}}}, nil
		})
	}
	for _, prompt := range []string{
		"speechkit_go_sdk_integration",
		"speechkit_http_integration",
		"speechkit_server_setup",
		"speechkit_windows_client_to_server_setup",
		"speechkit_feature_integration",
		"speechkit_deployment_diagnosis",
	} {
		prompt := prompt
		server.AddPrompt(&mcp.Prompt{Name: prompt, Description: "SpeechKit integration prompt"}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Description: prompt,
				Messages: []*mcp.PromptMessage{{
					Role:    "user",
					Content: &mcp.TextContent{Text: promptText(prompt)},
				}},
			}, nil
		})
	}
}

func (a *speechkitMCP) addManagement(server *mcp.Server) {
	mcp.AddTool(server, mcpTool("speechkit_status", "GET /readyz", true, false, true), a.status)
	mcp.AddTool(server, mcpTool("speechkit_config_get", "GET /v1/config", true, false, true), a.configGet)
	mcp.AddTool(server, mcpTool("speechkit_provider_list", "GET /v1/catalog/profiles", true, false, true), a.providerList)
	mcp.AddTool(server, mcpTool("speechkit_provider_readiness", "GET /v1/catalog/profiles/{id}/readiness", true, false, true), a.providerReadiness)
	mcp.AddTool(server, mcpTool("speechkit_personas_list", "GET /v1/personas", true, false, true), a.personasList)
	mcp.AddTool(server, mcpTool("speechkit_persona_get", "GET /v1/personas/{id}", true, false, true), a.personaGet)
	mcp.AddTool(server, mcpTool("speechkit_persona_create", "POST /v1/personas", false, false, false), a.personaCreate)
	mcp.AddTool(server, mcpTool("speechkit_persona_update", "PATCH /v1/personas/{id}", false, false, true), a.personaUpdate)
	mcp.AddTool(server, mcpTool("speechkit_persona_delete", "DELETE /v1/personas/{id}", false, true, true), a.personaDelete)
	mcp.AddTool(server, mcpTool("speechkit_roles_list", "GET /v1/roles", true, false, true), a.rolesList)
	mcp.AddTool(server, mcpTool("speechkit_role_get", "GET /v1/roles/{id}", true, false, true), a.roleGet)
	mcp.AddTool(server, mcpTool("speechkit_role_create", "POST /v1/roles", false, false, false), a.roleCreate)
	mcp.AddTool(server, mcpTool("speechkit_role_update", "PATCH /v1/roles/{id}", false, false, true), a.roleUpdate)
	mcp.AddTool(server, mcpTool("speechkit_role_delete", "DELETE /v1/roles/{id}", false, true, true), a.roleDelete)
	mcp.AddTool(server, mcpTool("speechkit_sequences_list", "GET /v1/sequences", true, false, true), a.sequencesList)
	mcp.AddTool(server, mcpTool("speechkit_sequence_get", "GET /v1/sequences/{id}", true, false, true), a.sequenceGet)
	mcp.AddTool(server, mcpTool("speechkit_sequence_create", "POST /v1/sequences", false, false, false), a.sequenceCreate)
	mcp.AddTool(server, mcpTool("speechkit_sequence_update", "PATCH /v1/sequences/{id}", false, false, true), a.sequenceUpdate)
	mcp.AddTool(server, mcpTool("speechkit_sequence_delete", "DELETE /v1/sequences/{id}", false, true, true), a.sequenceDelete)
	mcp.AddTool(server, mcpTool("speechkit_transcripts_list", "GET /v1/transcripts", true, false, true), a.transcriptsList)
	mcp.AddTool(server, mcpTool("speechkit_transcript_get", "GET /v1/transcripts/{id}", true, false, true), a.transcriptGet)
	mcp.AddTool(server, mcpTool("speechkit_voiceagent_session_summary", "GET /v1/voiceagent/sessions/{id}/summary", true, false, true), a.voiceAgentSessionSummary)
	mcp.AddTool(server, mcpTool("speechkit_vocabulary_get", "GET /v1/vocabulary/dictionary", true, false, true), a.vocabularyGet)
	mcp.AddTool(server, mcpTool("speechkit_vocabulary_replace", "POST /v1/vocabulary/dictionary", false, true, true), a.vocabularyReplace)
	mcp.AddTool(server, mcpTool("speechkit_transcribe", "Transcribe a local audio file via /v1/dictation/transcribe.", false, false, false), a.transcribe)
	mcp.AddTool(server, mcpTool("speechkit_tts_synthesize", "POST /v1/tts/synthesize", false, false, false), a.ttsSynthesize)
}

func (a *speechkitMCP) addTest(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "speechkit_validate_config", Description: "Validate a SpeechKit TOML config snippet."}, a.validateConfig)
	mcp.AddTool(server, &mcp.Tool{Name: "speechkit_validate_request", Description: "Validate request JSON shape heuristically against known OpenAPI paths."}, a.validateRequest)
	mcp.AddTool(server, &mcp.Tool{Name: "speechkit_validate_response", Description: "Validate response JSON shape heuristically against known OpenAPI paths."}, a.validateResponse)
	mcp.AddTool(server, &mcp.Tool{Name: "speechkit_check_compatibility", Description: "Heuristically check client code for known SpeechKit API paths."}, a.checkCompatibility)
	mcp.AddTool(server, &mcp.Tool{Name: "speechkit_list_breaking_changes", Description: "Return CHANGELOG snippets for a version range."}, a.breakingChanges)
	mcp.AddTool(server, mcpTool("speechkit_self_check_plan", "Return ordered health, readiness, config, OpenAPI, and AsyncAPI probes for a SpeechKit Server.", true, false, true), a.selfCheckPlan)
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

func loadDocs() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(speechkitdocs.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Broken embedded doc entries are ignored so one bad optional file does not disable MCP startup.
		}
		if d.IsDir() {
			return nil
		}
		raw, err := speechkitdocs.FS.ReadFile(path)
		if err == nil {
			out["docs/"+path] = string(raw)
		}
		return nil
	})
	return out
}

func openAPISpec() string {
	if raw, err := speechkitdocs.FS.ReadFile("server/openapi.v1.yaml"); err == nil {
		return string(raw)
	}
	return "openapi: 3.0.3\ninfo:\n  title: SpeechKit Server API\npaths: {}\n"
}

func asyncAPISpec() string {
	if raw, err := speechkitdocs.FS.ReadFile("server/asyncapi.v1.yaml"); err == nil {
		return string(raw)
	}
	return "asyncapi: 3.0.0\ninfo:\n  title: SpeechKit Voice Agent WebSocket API\nchannels: {}\noperations: {}\n"
}

func loadOpenAPIDocument(ctx context.Context) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(openAPISpec()))
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI: %w", err)
	}
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate embedded OpenAPI: %w", err)
	}
	return doc, nil
}

func inferMethod(doc *openapi3.T, endpoint, kind string) string {
	if doc == nil || doc.Paths == nil {
		return ""
	}
	item := doc.Paths.Value(endpoint)
	if item == nil {
		return ""
	}
	if kind == "request" {
		for _, candidate := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodGet} {
			if item.GetOperation(candidate) != nil {
				return candidate
			}
		}
	}
	for _, candidate := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if item.GetOperation(candidate) != nil {
			return candidate
		}
	}
	return ""
}

func endpointSnippet(spec, path string) string {
	idx := strings.Index(spec, "\n  "+path+":")
	if idx < 0 {
		idx = strings.Index(spec, "\n  \""+path+"\":")
	}
	if idx < 0 {
		return ""
	}
	end := strings.Index(spec[idx+1:], "\n  /")
	if end < 0 {
		end = min(len(spec)-idx, 2500)
	} else {
		end++
	}
	return strings.TrimSpace(spec[idx : idx+end])
}

func openAPIEndpoints(spec string) []string {
	re := regexp.MustCompile(`(?m)^ {2}(/[^:]+):`)
	matches := re.FindAllStringSubmatch(spec, -1)
	endpoints := make([]string, 0, len(matches))
	for _, m := range matches {
		endpoints = append(endpoints, strings.TrimSpace(m[1]))
	}
	sort.Strings(endpoints)
	return endpoints
}

func mcpTool(name, description string, readOnly, destructive, idempotent bool) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Title:       strings.TrimPrefix(strings.ReplaceAll(name, "_", " "), "speechkit "),
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			Title:           strings.TrimPrefix(strings.ReplaceAll(name, "_", " "), "speechkit "),
			ReadOnlyHint:    readOnly,
			DestructiveHint: boolPtr(destructive),
			IdempotentHint:  idempotent,
			OpenWorldHint:   boolPtr(false),
		},
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func resourceTitle(uri string) string {
	base := strings.TrimSuffix(uri, ".md")
	base = strings.TrimSuffix(base, ".yaml")
	base = strings.ReplaceAll(base, "/", " ")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func mimeTypeForResource(uri string) string {
	if strings.HasSuffix(uri, ".yaml") || strings.HasSuffix(uri, ".yml") {
		return "application/yaml"
	}
	return "text/markdown"
}

func resourcePriority(uri string) float64 {
	switch uri {
	case "docs/server/openapi.v1.yaml", "docs/server/asyncapi.v1.yaml":
		return 1.0
	case "docs/speechkit-architecture-v2.md", "docs/mcp/README.md":
		return 0.9
	default:
		return 0.6
	}
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSpace(strings.Split(addr, ":")[0])
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func requireMCPToken(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if got == "" {
			got = strings.TrimSpace(r.Header.Get("X-SpeechKit-MCP-Token"))
		}
		if !mcpTokenMatches(token, got) {
			http.Error(w, "mcp token required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mcpTokenMatches(expected, presented string) bool {
	expected = strings.TrimSpace(expected)
	presented = strings.TrimSpace(presented)
	if expected == "" || presented == "" {
		return false
	}
	expectedHash := sha256.Sum256([]byte(expected))
	presentedHash := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(expectedHash[:], presentedHash[:]) == 1
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func jsonResult(value any) *mcp.CallToolResult {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return textResult(string(raw))
}

func parseModes(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func promptText(name string) string {
	switch name {
	case "speechkit_server_setup":
		return "Hi Codex, go to speechkit.cc and install the SpeechKit Server on this server. First read https://speechkit.cc/llms.txt and https://speechkit.cc/install/server.md. Use the stable channel unless the user explicitly asks for a preview build; for preview installs use https://speechkit.cc/install-server.sh with --channel preview. Do not create release tags or mutate GHCR latest for preview testing."
	case "speechkit_go_sdk_integration":
		return "Hi Codex, add SpeechKit as a Go framework dependency and use the documented Dictation, Assist, and Voice Agent contracts. Start with https://speechkit.cc/llms.txt, then prefer pkg/speechkit for embedded Go hosts and pkg/speechkit/client for a running SpeechKit Server."
	case "speechkit_http_integration":
		return "Use the SpeechKit OpenAPI contract at https://speechkit.cc/api/openapi.v1.yaml before writing HTTP code, and the Voice Agent AsyncAPI contract at https://speechkit.cc/api/asyncapi.v1.yaml before writing WebSocket clients. Prefer canonical /v1 paths for Dictation, Assist, Voice Agent, catalog, config, vocabulary, transcript, and TTS calls."
	case "speechkit_windows_client_to_server_setup":
		return "Connect the Local Windows Client to a SpeechKit Server only after the server passes /healthz and /readyz. Keep mode_source and server_connection settings explicit, and keep bearer tokens in environment variables."
	case "speechkit_feature_integration":
		return "Use SpeechKit's strict mode boundaries: Dictation is STT only, Assist returns a one-shot utility or LLM result, and Voice Agent is realtime dialogue. Read https://speechkit.cc/getting-started/technical.md and validate API payloads with this MCP server before editing integration code."
	case "speechkit_deployment_diagnosis":
		return "Diagnose SpeechKit deployments by checking Docker Compose state, /healthz, /readyz, server logs, configured provider secrets, https://speechkit.cc/api/openapi.v1.yaml, and https://speechkit.cc/api/asyncapi.v1.yaml. Do not switch auth_mode to none on a public host."
	default:
		return "Use the SpeechKit MCP docs and OpenAPI tools to complete: " + strings.ReplaceAll(name, "_", " ") + ". Prefer /v1 canonical paths, https://speechkit.cc/llms.txt, and pkg/speechkit/client for Go."
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

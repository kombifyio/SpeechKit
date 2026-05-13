package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

// Server wiring for the MCP command. Protocol handlers stay separate from
// process startup so tests can construct handlers without running main().
type speechkitMCP struct {
	opts   serverOptions
	client *skclient.Client
	docs   map[string]string
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

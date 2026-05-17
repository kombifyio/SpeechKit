package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	speechkitdocs "github.com/kombifyio/SpeechKit/docs"
)

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
	raw, err := speechkitdocs.FS.ReadFile("server/openapi.v1.yaml")
	if err != nil {
		panic("embedded SpeechKit OpenAPI spec unavailable: " + err.Error())
	}
	return string(raw)
}

func asyncAPISpec() string {
	raw, err := speechkitdocs.FS.ReadFile("server/asyncapi.v1.yaml")
	if err != nil {
		panic("embedded SpeechKit AsyncAPI spec unavailable: " + err.Error())
	}
	return string(raw)
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
	if strings.HasSuffix(uri, ".json") {
		return "application/json"
	}
	if strings.HasSuffix(uri, ".toml") {
		return "application/toml"
	}
	return "text/markdown"
}

func resourcePriority(uri string) float64 {
	switch uri {
	case "docs/server/openapi.v1.yaml", "docs/server/asyncapi.v1.yaml":
		return 1.0
	case "docs/agent/schemas/speechkit-one-shot-manifest.schema.json", "docs/agent/schemas/speechkit-one-shot-functional-result.schema.json":
		return 0.95
	case "docs/speechkit-architecture-v2.md", "docs/mcp/README.md":
		return 0.9
	default:
		return 0.6
	}
}

func promptText(name string) string {
	switch name {
	case "speechkit_server_setup":
		return "Hi Codex, go to speechkit.cc and install the SpeechKit Server on this server. First read https://speechkit.cc/llms.txt and https://speechkit.cc/install/server.md. For browser Docker Compose use https://speechkit.cc/install-server/docker-compose.example.yml and set SPEECHKIT_PUBLIC_URL. Use the stable channel unless the user explicitly asks for a preview build; for preview installs use https://speechkit.cc/install-server.sh with --channel preview. Do not create release tags or mutate GHCR latest for preview testing."
	case "speechkit_go_sdk_integration":
		return "Hi Codex, add SpeechKit as a Go framework dependency and use the documented Dictation, Assist, and Voice Agent contracts. Start with https://speechkit.cc/llms.txt, then prefer pkg/speechkit for embedded Go hosts and pkg/speechkit/client for a running SpeechKit Server."
	case "speechkit_http_integration":
		return "Use the SpeechKit OpenAPI contract at https://speechkit.cc/api/openapi.v1.yaml before writing HTTP code, and the Voice Agent AsyncAPI contract at https://speechkit.cc/api/asyncapi.v1.yaml before writing WebSocket clients. Browser WebSocket auth uses the returned ?ticket query, not a bearer header. Prefer canonical /v1 paths and voiceagent spelling for Dictation, Assist, Voice Agent, catalog, config, vocabulary, transcript, and TTS calls."
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

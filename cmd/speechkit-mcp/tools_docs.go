package main

import (
	"context"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcputil "github.com/kombifyio/SpeechKit/cmd/speechkit-mcp/internal/util"
	"github.com/kombifyio/SpeechKit/internal/scaffold"
)

func (a *speechkitMCP) docsSearch(ctx context.Context, req *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, any, error) {
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" {
		return mcputil.TextResult("query is required"), nil, nil
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
	return mcputil.JSONResult(hits), hits, nil
}

func (a *speechkitMCP) apiEndpoint(ctx context.Context, req *mcp.CallToolRequest, in endpointInput) (*mcp.CallToolResult, any, error) {
	spec := openAPISpec()
	snippet := endpointSnippet(spec, in.Path)
	if snippet == "" {
		return mcputil.TextResult("endpoint not found in OpenAPI spec"), nil, nil
	}
	return mcputil.TextResult(snippet), map[string]string{"path": in.Path, "snippet": snippet}, nil
}

func (a *speechkitMCP) apiOverview(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	endpoints := openAPIEndpoints(openAPISpec())
	return mcputil.JSONResult(endpoints), endpoints, nil
}

func (a *speechkitMCP) getOpenAPISpec(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return mcputil.TextResult(openAPISpec()), nil, nil
}

func (a *speechkitMCP) getAsyncAPISpec(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return mcputil.TextResult(asyncAPISpec()), nil, nil
}

func (a *speechkitMCP) getDictationStreamAsyncAPISpec(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return mcputil.TextResult(dictationStreamAsyncAPISpec()), nil, nil
}

func (a *speechkitMCP) integrationExample(ctx context.Context, req *mcp.CallToolRequest, in integrationInput) (*mcp.CallToolResult, any, error) {
	language := strings.ToLower(strings.TrimSpace(in.Language))
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "dictation"
	}
	key := "docs/mcp/examples/" + language + "/" + mode + ".md"
	if body, ok := a.docs[key]; ok {
		return mcputil.TextResult(body), map[string]string{"language": in.Language, "mode": in.Mode, "example": body}, nil
	}
	return mcputil.TextResult("No embedded example for language " + in.Language), nil, nil
}

func (a *speechkitMCP) architectureOverview(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return mcputil.TextResult(a.docs["docs/architecture/sdk-surface-boundary.md"] + "\n\n" + a.docs["docs/speechkit-framework-api.md"] + "\n\n" + a.docs["docs/mcp/README.md"]), nil, nil
}

func (a *speechkitMCP) scaffoldTemplates(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	templates, err := scaffold.ListTemplates()
	if err != nil {
		return nil, nil, err
	}
	out := map[string]any{"templates": templates}
	return mcputil.JSONResult(out), out, nil
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
	return mcputil.JSONResult(out), out, nil
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
		args = append(args, "--dir", mcputil.ShellQuote(installDir))
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
	return mcputil.JSONResult(out), out, nil
}

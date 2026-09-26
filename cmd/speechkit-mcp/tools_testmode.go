package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcputil "github.com/kombifyio/SpeechKit/cmd/speechkit-mcp/internal/util"
	"github.com/kombifyio/SpeechKit/internal/config"
)

func (a *speechkitMCP) validateConfig(ctx context.Context, req *mcp.CallToolRequest, in configValidationInput) (*mcp.CallToolResult, any, error) {
	var cfg config.Config
	_, err := toml.Decode(in.TOML, &cfg)
	out := map[string]any{"valid": err == nil}
	if err != nil {
		out["error"] = err.Error()
	}
	return mcputil.JSONResult(out), out, nil
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
	return mcputil.JSONResult(out), out, nil
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
	return mcputil.JSONResult(out), out, nil
}

func (a *speechkitMCP) breakingChanges(ctx context.Context, req *mcp.CallToolRequest, in changesInput) (*mcp.CallToolResult, any, error) {
	raw, readErr := os.ReadFile("CHANGELOG.md")
	if readErr == nil {
		text := string(raw)
		if len(text) > 8000 {
			text = text[:8000]
		}
		return mcputil.TextResult(text), map[string]string{"from": in.FromVersion, "to": in.ToVersion}, nil
	}
	return mcputil.TextResult("No CHANGELOG.md found in current working directory."), nil, nil
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
		return mcputil.JSONResult(out), out, nil
	}
	if len(bytes.TrimSpace(in.Payload)) > 0 && !json.Valid(in.Payload) {
		out["valid"] = false
		out["error"] = "payload is not valid JSON"
		return mcputil.JSONResult(out), out, nil
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
		return mcputil.JSONResult(out), out, nil
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
		return mcputil.JSONResult(out), out, nil //nolint:nilerr // A schema mismatch is returned as tool data, not an MCP transport failure.
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
	return mcputil.JSONResult(out), out, nil
}

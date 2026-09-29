package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v3"

	speechkitdocs "github.com/kombifyio/SpeechKit/docs"
)

// Invariant: every operation the server contract annotates as an agent tool
// (x-kombify-mcp in docs/server/openapi.v1.yaml, API-FIRST-STANDARD §9) is
// served by speechkit-mcp management mode under the same name with the same
// safety hints, so the contract and the MCP connector cannot drift apart.
func TestContractAnnotatedToolsMatchServedTools(t *testing.T) {
	raw, err := speechkitdocs.FS.ReadFile("server/openapi.v1.yaml")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	type hints struct {
		ReadOnly    bool `yaml:"readOnlyHint"`
		Destructive bool `yaml:"destructiveHint"`
		Idempotent  bool `yaml:"idempotentHint"`
		OpenWorld   bool `yaml:"openWorldHint"`
	}
	var spec struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	contract := map[string]hints{}
	for path, item := range spec.Paths {
		for method, node := range item {
			var op struct {
				OperationID string    `yaml:"operationId"`
				MCP         yaml.Node `yaml:"x-kombify-mcp"`
			}
			if node.Kind != yaml.MappingNode || node.Decode(&op) != nil || op.MCP.Kind != yaml.MappingNode {
				continue
			}
			var tool struct {
				ToolName    string `yaml:"toolName"`
				Annotations hints  `yaml:"annotations"`
			}
			if err := op.MCP.Decode(&tool); err != nil || tool.ToolName == "" {
				t.Fatalf("%s %s: x-kombify-mcp needs an explicit toolName: %v", method, path, err)
			}
			contract[tool.ToolName] = tool.Annotations
		}
	}

	ctx := context.Background()
	app := &speechkitMCP{opts: serverOptions{modes: map[string]bool{"management": true}}}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := app.newServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "contract-test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()
	served := map[string]hints{}
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		a := tool.Annotations
		served[tool.Name] = hints{
			ReadOnly:    a.ReadOnlyHint,
			Destructive: a.DestructiveHint != nil && *a.DestructiveHint,
			Idempotent:  a.IdempotentHint,
			OpenWorld:   a.OpenWorldHint != nil && *a.OpenWorldHint,
		}
	}
	for name, want := range contract {
		got, ok := served[name]
		if !ok {
			t.Errorf("contract tool %s is not served by speechkit-mcp management mode", name)
			continue
		}
		if got != want {
			t.Errorf("%s: served hints %+v, contract says %+v", name, got, want)
		}
	}
}

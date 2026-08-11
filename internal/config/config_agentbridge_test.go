package config

import (
	"strings"
	"testing"
)

func TestAgentBridgeNormalizeDefaults(t *testing.T) {
	var c AgentBridgeConfig
	c.Normalize()
	if c.Enabled || c.Codex.Enabled {
		t.Fatal("agent bridge must default OFF (fail-closed)")
	}
	if c.Codex.Mode != AgentBridgeModeAuto {
		t.Fatalf("mode = %q, want auto", c.Codex.Mode)
	}
	if c.Codex.Sandbox != AgentBridgeSandboxReadOnly {
		t.Fatalf("sandbox = %q, want read-only", c.Codex.Sandbox)
	}
	if c.Codex.ApprovalTimeoutSec != 120 || c.Codex.MaxConcurrentTurns != 1 || c.Codex.Narration != "summary" {
		t.Fatalf("defaults wrong: %+v", c.Codex)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("normalized zero value must validate: %v", err)
	}
}

func TestAgentBridgeValidateRejectsUnknownSandbox(t *testing.T) {
	var c AgentBridgeConfig
	c.Normalize()
	c.Codex.Sandbox = "danger-full-access"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "read-only|workspace-write") {
		t.Fatalf("danger-full-access must be unrepresentable, got %v", err)
	}
}

func TestAgentBridgeValidateWorkspaceWriteRequiresProjectGrant(t *testing.T) {
	var c AgentBridgeConfig
	c.Normalize()
	c.Enabled = true
	c.Codex.Enabled = true
	c.Codex.Sandbox = AgentBridgeSandboxWorkspaceWrite
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "project entry granting workspace-write") {
		t.Fatalf("workspace-write without a project grant must fail, got %v", err)
	}
}

func TestAgentBridgeValidateWorkspaceWriteRequiresEnableFlags(t *testing.T) {
	var c AgentBridgeConfig
	c.Normalize()
	c.Codex.Sandbox = AgentBridgeSandboxWorkspaceWrite
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "agent_bridge.enabled") {
		t.Fatalf("workspace-write without enable flags must fail, got %v", err)
	}
}

func TestAgentBridgeValidateProjects(t *testing.T) {
	dir := t.TempDir()
	var c AgentBridgeConfig
	c.Enabled = true
	c.Codex.Enabled = true
	c.Codex.Projects = []AgentBridgeProject{
		{Alias: "one", Path: dir},
		{Alias: "one", Path: dir},
	}
	c.Normalize()
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("duplicate alias must fail, got %v", err)
	}

	c.Codex.Projects = []AgentBridgeProject{{Alias: "ghost", Path: dir + "-does-not-exist"}}
	c.Normalize()
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "existing directory") {
		t.Fatalf("missing path must fail, got %v", err)
	}

	c.Codex.Projects = []AgentBridgeProject{{Alias: "wide", Path: dir, Sandbox: AgentBridgeSandboxWorkspaceWrite}}
	c.Normalize()
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "global agent_bridge.codex.sandbox ceiling") {
		t.Fatalf("project may not widen beyond the global ceiling, got %v", err)
	}

	c.Codex.Sandbox = AgentBridgeSandboxWorkspaceWrite
	if err := c.Validate(); err != nil {
		t.Fatalf("explicit double-enabled workspace-write grant must validate, got %v", err)
	}

	if _, ok := c.ProjectByAlias("WIDE"); !ok {
		t.Fatal("ProjectByAlias must match case-insensitively")
	}
	if _, ok := c.ProjectByAlias("nope"); ok {
		t.Fatal("unknown alias must fail closed")
	}
}

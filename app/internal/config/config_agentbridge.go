package config

import (
	"fmt"
	"os"
	"strings"
)

// AgentBridgeConfig is the additive [agent_bridge] block for the External
// Coding Agent Bridge (AI-VOICE-SPEECHKIT-TARGET.md, adopted 2026-08-10).
// Fail-closed by construction: the bridge is off unless BOTH the master
// switch and a per-agent switch are enabled, side effects additionally
// require an explicit per-project allowlist entry, and "danger-full-access"
// is unrepresentable. The Server-Target ignores this block entirely.
type AgentBridgeConfig struct {
	Enabled bool              `toml:"enabled"`
	Codex   CodexBridgeConfig `toml:"codex"`
}

// CodexBridgeConfig configures the Codex implementation of the bridge.
type CodexBridgeConfig struct {
	Enabled            bool                 `toml:"enabled"`
	BinaryPath         string               `toml:"binary_path"`          // empty = PATH lookup
	Mode               string               `toml:"mode"`                 // auto | app_server | exec
	Sandbox            string               `toml:"sandbox"`              // global ceiling: read-only | workspace-write
	ApprovalTimeoutSec int                  `toml:"approval_timeout_sec"` // unanswered approval card => deny
	Narration          string               `toml:"narration"`            // off | summary | verbose
	MaxConcurrentTurns int                  `toml:"max_concurrent_turns"`
	Projects           []AgentBridgeProject `toml:"projects"`
}

// AgentBridgeProject is one allowlisted working directory.
type AgentBridgeProject struct {
	Alias   string `toml:"alias"`
	Path    string `toml:"path"`
	Sandbox string `toml:"sandbox"` // effective = min(global, project); empty inherits read-only
}

const (
	AgentBridgeSandboxReadOnly       = "read-only"
	AgentBridgeSandboxWorkspaceWrite = "workspace-write"

	AgentBridgeModeAuto      = "auto"
	AgentBridgeModeAppServer = "app_server"
	AgentBridgeModeExec      = "exec"
)

// Normalize fills defaults on the zero value so a missing [agent_bridge]
// block behaves identically to an explicit default-off one.
func (c *AgentBridgeConfig) Normalize() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Codex.Mode) == "" {
		c.Codex.Mode = AgentBridgeModeAuto
	}
	if strings.TrimSpace(c.Codex.Sandbox) == "" {
		c.Codex.Sandbox = AgentBridgeSandboxReadOnly
	}
	if c.Codex.ApprovalTimeoutSec <= 0 {
		c.Codex.ApprovalTimeoutSec = 120
	}
	if strings.TrimSpace(c.Codex.Narration) == "" {
		c.Codex.Narration = "summary"
	}
	if c.Codex.MaxConcurrentTurns <= 0 {
		c.Codex.MaxConcurrentTurns = 1
	}
	for i := range c.Codex.Projects {
		if strings.TrimSpace(c.Codex.Projects[i].Sandbox) == "" {
			c.Codex.Projects[i].Sandbox = AgentBridgeSandboxReadOnly
		}
	}
}

// Validate reports configuration errors. It is deliberately strict:
// anything outside the two representable sandbox levels is rejected (there
// is no spelling of danger-full-access), and workspace-write demands both
// enable flags plus at least one explicit project entry carrying it.
func (c *AgentBridgeConfig) Validate() error {
	if c == nil {
		return nil
	}
	switch c.Codex.Mode {
	case AgentBridgeModeAuto, AgentBridgeModeAppServer, AgentBridgeModeExec:
	default:
		return fmt.Errorf("agent_bridge.codex.mode %q is not one of auto|app_server|exec", c.Codex.Mode)
	}
	if err := validateAgentBridgeSandbox("agent_bridge.codex.sandbox", c.Codex.Sandbox); err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, p := range c.Codex.Projects {
		alias := strings.TrimSpace(p.Alias)
		if alias == "" {
			return fmt.Errorf("agent_bridge.codex.projects[%d]: alias must not be empty", i)
		}
		if seen[alias] {
			return fmt.Errorf("agent_bridge.codex.projects: alias %q is not unique", alias)
		}
		seen[alias] = true
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return fmt.Errorf("agent_bridge.codex.projects[%d] (%s): path must not be empty", i, alias)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return fmt.Errorf("agent_bridge.codex.projects[%d] (%s): path %q is not an existing directory", i, alias, path)
		}
		if err := validateAgentBridgeSandbox(fmt.Sprintf("agent_bridge.codex.projects[%d].sandbox", i), p.Sandbox); err != nil {
			return err
		}
		// A project may not widen beyond the global ceiling.
		if p.Sandbox == AgentBridgeSandboxWorkspaceWrite && c.Codex.Sandbox != AgentBridgeSandboxWorkspaceWrite {
			return fmt.Errorf("agent_bridge.codex.projects[%d] (%s): workspace-write requires the global agent_bridge.codex.sandbox ceiling to be workspace-write too", i, alias)
		}
	}
	// workspace-write is only meaningful as an explicit, double-enabled,
	// per-project grant. A global ceiling without any granted project (or
	// without the enable flags) is a misconfiguration, not a silent no-op.
	if c.Codex.Sandbox == AgentBridgeSandboxWorkspaceWrite {
		if !c.Enabled || !c.Codex.Enabled {
			return fmt.Errorf("agent_bridge.codex.sandbox workspace-write requires agent_bridge.enabled and agent_bridge.codex.enabled")
		}
		granted := false
		for _, p := range c.Codex.Projects {
			if p.Sandbox == AgentBridgeSandboxWorkspaceWrite {
				granted = true
				break
			}
		}
		if !granted {
			return fmt.Errorf("agent_bridge.codex.sandbox workspace-write requires at least one project entry granting workspace-write explicitly")
		}
	}
	return nil
}

func validateAgentBridgeSandbox(field, value string) error {
	switch value {
	case AgentBridgeSandboxReadOnly, AgentBridgeSandboxWorkspaceWrite:
		return nil
	default:
		return fmt.Errorf("%s %q is not one of read-only|workspace-write", field, value)
	}
}

// ProjectByAlias resolves an allowlisted project. The boolean is false for
// unknown aliases — callers fail closed.
func (c *AgentBridgeConfig) ProjectByAlias(alias string) (AgentBridgeProject, bool) {
	if c == nil {
		return AgentBridgeProject{}, false
	}
	alias = strings.TrimSpace(alias)
	for _, p := range c.Codex.Projects {
		if strings.EqualFold(p.Alias, alias) {
			return p, true
		}
	}
	return AgentBridgeProject{}, false
}

// BridgeActive reports whether the double enable gate is open.
func (c *AgentBridgeConfig) BridgeActive() bool {
	return c != nil && c.Enabled && c.Codex.Enabled
}

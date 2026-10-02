package util

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TextResult returns a plain-text MCP tool result.
func TextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// JSONResult returns a JSON-formatted MCP tool result.
func JSONResult(value any) *mcp.CallToolResult {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return TextResult(string(raw))
}

// StringMapValue reads and trims a string-like value from generic JSON input.
func StringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// ShellQuote returns a POSIX-safe single-quoted shell argument.
func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

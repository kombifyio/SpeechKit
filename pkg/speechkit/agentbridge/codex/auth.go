package codex

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// authInfo is the detection-only view of the Codex CLI's credential cache.
// The bridge never implements the OAuth flow, never refreshes tokens, and
// never logs or copies token material — the supported strategy is "run the
// Codex binary and let it persist auth.json" (learn.chatgpt.com/docs/auth).
type authInfo struct {
	Method agentbridge.AuthMethod
	Plan   string
}

// codexHome resolves the Codex state directory: an explicit override (tests),
// else $CODEX_HOME, else ~/.codex.
func codexHome(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// authFileShape mirrors only the fields detection needs; unknown fields are
// ignored so future Codex versions cannot break detection.
type authFileShape struct {
	OpenAIAPIKey *string `json:"OPENAI_API_KEY"`
	Tokens       *struct {
		IDToken string `json:"id_token"`
	} `json:"tokens"`
}

// detectAuth classifies the Codex sign-in state from auth.json metadata.
func detectAuth(home string) authInfo {
	if home == "" {
		return authInfo{Method: agentbridge.AuthNone}
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json")) //nolint:gosec // G304: fixed filename under the configured Codex home, not caller input
	if err != nil {
		return authInfo{Method: agentbridge.AuthNone}
	}
	var shape authFileShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return authInfo{Method: agentbridge.AuthNone}
	}
	if shape.Tokens != nil && strings.TrimSpace(shape.Tokens.IDToken) != "" {
		return authInfo{Method: agentbridge.AuthChatGPT, Plan: planFromIDToken(shape.Tokens.IDToken)}
	}
	if shape.OpenAIAPIKey != nil && strings.TrimSpace(*shape.OpenAIAPIKey) != "" {
		return authInfo{Method: agentbridge.AuthAPIKey}
	}
	return authInfo{Method: agentbridge.AuthNone}
}

// planFromIDToken best-effort extracts the ChatGPT plan label from the ID
// token's payload claims WITHOUT verifying the signature — this is display
// metadata, never an authorization input. Returns "" when absent.
func planFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTPlanType string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(claims.Auth.ChatGPTPlanType))
}

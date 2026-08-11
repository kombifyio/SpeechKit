package codex

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

func writeAuthFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fakeIDToken(t *testing.T, plan string) string {
	t.Helper()
	payload := `{"https://api.openai.com/auth":{"chatgpt_plan_type":"` + plan + `"}}`
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

func TestDetectAuth(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, dir string)
		wantAuth agentbridge.AuthMethod
		wantPlan string
	}{
		{
			name:     "missing auth.json means not signed in",
			setup:    func(*testing.T, string) {},
			wantAuth: agentbridge.AuthNone,
		},
		{
			name: "chatgpt tokens win and carry the plan label",
			setup: func(t *testing.T, dir string) {
				writeAuthFile(t, dir, `{"OPENAI_API_KEY":null,"tokens":{"id_token":"`+fakeIDToken(t, "Pro")+`"}}`)
			},
			wantAuth: agentbridge.AuthChatGPT,
			wantPlan: "pro",
		},
		{
			name: "api key without tokens",
			setup: func(t *testing.T, dir string) {
				writeAuthFile(t, dir, `{"OPENAI_API_KEY":"sk-fake"}`)
			},
			wantAuth: agentbridge.AuthAPIKey,
		},
		{
			name: "empty file body means not signed in",
			setup: func(t *testing.T, dir string) {
				writeAuthFile(t, dir, `{}`)
			},
			wantAuth: agentbridge.AuthNone,
		},
		{
			name: "corrupt json degrades to none, never panics",
			setup: func(t *testing.T, dir string) {
				writeAuthFile(t, dir, `{not json`)
			},
			wantAuth: agentbridge.AuthNone,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			got := detectAuth(dir)
			if got.Method != tc.wantAuth {
				t.Fatalf("auth method = %s, want %s", got.Method, tc.wantAuth)
			}
			if got.Plan != tc.wantPlan {
				t.Fatalf("plan = %q, want %q", got.Plan, tc.wantPlan)
			}
		})
	}
}

func TestCodexHomePrecedence(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	if got := codexHome("/explicit"); got != "/explicit" {
		t.Fatalf("explicit override must win, got %q", got)
	}
	t.Setenv("CODEX_HOME", "/from-env")
	if got := codexHome(""); got != "/from-env" {
		t.Fatalf("CODEX_HOME must win over the home default, got %q", got)
	}
}

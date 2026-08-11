package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// buildFakeCodex compiles the fixture binary once per test run. CI never
// needs a real Codex install or subscription.
func buildFakeCodex(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fakecodex")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "github.com/kombifyio/SpeechKit/tools/fakecodex")
	cmd.Env = os.Environ()
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fakecodex: %v\n%s", err, raw)
	}
	return out
}

func TestBridgeExecTurnAgainstFakeCodex(t *testing.T) {
	binary := buildFakeCodex(t)
	script, err := filepath.Abs(filepath.Join("testdata", "simple-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKECODEX_SCRIPT", script)
	// Point auth detection at a signed-in fake home so Status reaches exec
	// mode without touching the developer's real ~/.codex.
	home := t.TempDir()
	writeAuthFile(t, home, `{"OPENAI_API_KEY":null,"tokens":{"id_token":"`+fakeIDToken(t, "plus")+`"}}`)

	bridge := New(Config{BinaryPath: binary, CodexHome: home, Mode: "exec"})
	defer bridge.Close()

	st := bridge.Status(context.Background())
	if st.Mode != agentbridge.ModeExec {
		t.Fatalf("status mode = %s (detail %q), want exec", st.Mode, st.Detail)
	}
	if st.Auth != agentbridge.AuthChatGPT || st.Plan != "plus" {
		t.Fatalf("auth = %s plan = %q, want chatgpt/plus", st.Auth, st.Plan)
	}

	started := time.Now()
	_, err = bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir(), Sandbox: agentbridge.SandboxReadOnly},
		Prompt:  "run the fixture",
	})
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if ack := time.Since(started); ack > time.Second {
		t.Fatalf("StartTurn ack took %s, must stay under 1s (fast-ack contract)", ack)
	}

	var (
		types    []agentbridge.EventType
		threadID string
		items    int
	)
	deadline := time.After(15 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-bridge.Events():
			types = append(types, ev.Type)
			if ev.ThreadID != "" {
				threadID = ev.ThreadID
			}
			if ev.Item != nil {
				items++
			}
			if ev.Type == agentbridge.EventTurnCompleted || ev.Type == agentbridge.EventError {
				done = true
			}
		case <-deadline:
			t.Fatalf("no turn completion within deadline; events so far: %v", types)
		}
	}

	if threadID != "th_fake123" {
		t.Fatalf("thread id = %q, want th_fake123 (from the golden script)", threadID)
	}
	if types[0] != agentbridge.EventThreadStarted || types[len(types)-1] != agentbridge.EventTurnCompleted {
		t.Fatalf("event envelope wrong: %v", types)
	}
	if items != 5 {
		t.Fatalf("normalized %d item events, want 5 (unknown/noise lines must be skipped)", items)
	}
	if got := bridge.LastThreadRef().ThreadID; got != "th_fake123" {
		t.Fatalf("LastThreadRef = %q, want th_fake123", got)
	}
}

func TestBridgeStartTurnBusy(t *testing.T) {
	binary := buildFakeCodex(t)
	// A script the fake streams slowly enough to still be running when the
	// second StartTurn arrives.
	script := filepath.Join(t.TempDir(), "slow.jsonl")
	lines := `{"type":"thread.started","thread_id":"th_slow"}` + "\n"
	for i := 0; i < 200; i++ {
		lines += `{"type":"item.completed","item":{"item_type":"reasoning","text":"tick"}}` + "\n"
	}
	lines += `{"type":"turn.completed","thread_id":"th_slow"}` + "\n"
	if err := os.WriteFile(script, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKECODEX_SCRIPT", script)
	home := t.TempDir()
	writeAuthFile(t, home, `{"OPENAI_API_KEY":"sk-fake"}`)

	bridge := New(Config{BinaryPath: binary, CodexHome: home, EventBuffer: 8, Mode: "exec"})
	defer bridge.Close()

	if _, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir()}, Prompt: "one",
	}); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if _, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir()}, Prompt: "two",
	}); err != agentbridge.ErrBusy {
		t.Fatalf("second concurrent turn err = %v, want ErrBusy", err)
	}
}

func TestBridgeStatusNotSignedIn(t *testing.T) {
	binary := buildFakeCodex(t)
	bridge := New(Config{BinaryPath: binary, CodexHome: t.TempDir()})
	defer bridge.Close()
	st := bridge.Status(context.Background())
	if st.Mode != agentbridge.ModeUnavailable || st.Auth != agentbridge.AuthNone {
		t.Fatalf("status = %+v, want unavailable/none", st)
	}
	if _, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir()}, Prompt: "x",
	}); err != agentbridge.ErrNotSignedIn {
		t.Fatalf("err = %v, want ErrNotSignedIn", err)
	}
}

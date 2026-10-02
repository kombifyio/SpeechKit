package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// TestExecTurnPassesPromptOnStdinNotArgv pins the H4 fix: the model-authored
// prompt reaches codex on stdin and never as a command-line argument, so
// neither cmd.exe quoting (codex.cmd shims) nor codex's own option parser
// can interpret it.
func TestExecTurnPassesPromptOnStdinNotArgv(t *testing.T) {
	binary := buildFakeCodex(t)
	script, err := filepath.Abs(filepath.Join("testdata", "simple-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKECODEX_SCRIPT", script)
	home := t.TempDir()
	writeAuthFile(t, home, `{"OPENAI_API_KEY":"sk-fake"}`)

	for _, prompt := range []string{
		`fix it" & calc & rem`,
		`--dangerously-bypass-approvals-and-sandbox`,
	} {
		record := filepath.Join(t.TempDir(), "record.json")
		t.Setenv("FAKECODEX_RECORD", record)
		bridge := New(Config{BinaryPath: binary, CodexHome: home, Mode: "exec"})
		if _, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
			Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir()},
			Prompt:  prompt,
		}); err != nil {
			t.Fatalf("start turn: %v", err)
		}
		drainUntilTurnEnd(t, bridge)
		_ = bridge.Close()

		raw, err := os.ReadFile(record)
		if err != nil {
			t.Fatalf("fake codex did not record its invocation: %v", err)
		}
		var got struct {
			Args  []string `json:"args"`
			Stdin string   `json:"stdin"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Stdin != prompt {
			t.Fatalf("stdin = %q, want the prompt %q", got.Stdin, prompt)
		}
		for _, arg := range got.Args {
			if strings.Contains(arg, prompt) {
				t.Fatalf("prompt leaked into argv: %q", got.Args)
			}
		}
	}
}

// TestExecModeRefusesWorkspaceWrite: exec mode has no approval round-trip,
// so a side-effectful sandbox must never run there.
func TestExecModeRefusesWorkspaceWrite(t *testing.T) {
	binary := buildFakeCodex(t)
	record := filepath.Join(t.TempDir(), "record.json")
	t.Setenv("FAKECODEX_RECORD", record)
	home := t.TempDir()
	writeAuthFile(t, home, `{"OPENAI_API_KEY":"sk-fake"}`)
	bridge := New(Config{BinaryPath: binary, CodexHome: home, Mode: "exec"})
	defer bridge.Close()

	_, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir(), Sandbox: agentbridge.SandboxWorkspaceWrite},
		Prompt:  "edit files",
		Sandbox: agentbridge.SandboxWorkspaceWrite,
	})
	if !errors.Is(err, agentbridge.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if _, statErr := os.Stat(record); statErr == nil {
		t.Fatal("codex exec was launched for a workspace-write turn")
	}
}

// TestWindowsLaunchesOnlyNativeExe pins the H4 binary rule: on Windows a
// script shim (npm's codex.cmd and friends) is never launched, because
// CreateProcess would route it through cmd.exe or another script host.
func TestWindowsLaunchesOnlyNativeExe(t *testing.T) {
	for _, path := range []string{
		`C:\Users\u\AppData\Roaming\npm\codex.cmd`,
		`C:\Users\u\AppData\Roaming\npm\codex.CMD`,
		`C:\tools\codex.bat`,
		`C:\Users\u\AppData\Roaming\npm\codex.ps1`,
		`C:\tools\codex.vbs`,
		`C:\tools\codex.js`,
		`C:\Users\u\AppData\Roaming\npm\codex`,
	} {
		if err := windowsNativeBinary(path); !errors.Is(err, errBinaryNotNative) {
			t.Fatalf("%s: err = %v, want refusal", path, err)
		}
	}
	for _, path := range []string{`C:\tools\codex.exe`, `C:\tools\CODEX.EXE`} {
		if err := windowsNativeBinary(path); err != nil {
			t.Fatalf("%s: native exe refused: %v", path, err)
		}
	}
}

func drainUntilTurnEnd(t *testing.T, bridge *Bridge) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-bridge.Events():
			if ev.Type == agentbridge.EventTurnCompleted || ev.Type == agentbridge.EventError {
				return
			}
		case <-deadline:
			t.Fatal("no turn completion within deadline")
		}
	}
}

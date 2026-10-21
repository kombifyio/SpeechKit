package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression for the fresh-install failure where whisper-server fail-fasted
// with 0xc0000409 because the model lived under a Windows profile with an
// umlaut in its name. The argv handed to the child must stay ASCII-only.
func TestWhisperModelArgumentKeepsArgvASCII(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Kombächer", "models")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(dir, "ggml-small.bin")
	if err := os.WriteFile(model, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	arg, workDir := whisperModelArgument(model)
	if !isASCII(arg) {
		t.Fatalf("model argument %q is not ASCII-only", arg)
	}
	resolved := arg
	if workDir != "" {
		resolved = filepath.Join(workDir, arg)
	}
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("argument %q with dir %q does not resolve to the model: %v", arg, workDir, err)
	}
}

func TestWhisperModelArgumentLeavesASCIIPathsUntouched(t *testing.T) {
	model := filepath.Join(t.TempDir(), "models", "ggml-small.bin")
	arg, workDir := whisperModelArgument(model)
	if arg != model || workDir != "" {
		t.Fatalf("whisperModelArgument(%q) = (%q, %q), want the path unchanged and no working directory", model, arg, workDir)
	}
}

func TestDescribeProcessExitNamesWindowsFailFast(t *testing.T) {
	for _, code := range []int{0xc0000409, int(int32(-1073740791))} {
		got := describeExitCode(code)
		if !strings.Contains(got, "0xc0000409") || !strings.Contains(got, "non-ASCII model path") {
			t.Fatalf("describeExitCode(%d) = %q, want the fail-fast cause and the non-ASCII path hint", code, got)
		}
	}
	if got := describeExitCode(0xc000001d); !strings.Contains(got, "AVX") {
		t.Fatalf("describeExitCode(illegal instruction) = %q, want a CPU feature hint", got)
	}
	if got := describeExitCode(1); got != "exit code 1" {
		t.Fatalf("describeExitCode(1) = %q", got)
	}
	if got := describeProcessExit(errors.New("plain")); got != "" {
		t.Fatalf("describeProcessExit(non-exit error) = %q, want empty", got)
	}
}

package localllm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateModelPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemma-3-4b-it-Q4_K_M.gguf")
	if err := ValidateModelPath(path); err != nil {
		t.Fatalf("ValidateModelPath(%q): %v", path, err)
	}
}

func TestValidateModelPathRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		"",
		"gemma-3-4b-it-Q4_K_M.gguf",
		dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "model.gguf",
		filepath.Join(t.TempDir(), "model.bin"),
		filepath.Join(t.TempDir(), "-bad.gguf"),
	} {
		if err := ValidateModelPath(path); err == nil {
			t.Fatalf("ValidateModelPath(%q) succeeded, want error", path)
		}
	}
}

func TestFindServerBinaryUsesManagedInstallLocation(t *testing.T) {
	localAppData := t.TempDir()
	binDir := filepath.Join(localAppData, "SpeechKit", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "llama-server.exe"
	if runtime.GOOS != "windows" {
		name = "llama-server"
	}
	want := filepath.Join(binDir, name)
	if err := os.WriteFile(want, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOCALAPPDATA", localAppData)
	got, err := FindServerBinary()
	if err != nil {
		t.Fatalf("FindServerBinary: %v", err)
	}
	if got != want {
		t.Fatalf("binary path = %q, want %q", got, want)
	}
}

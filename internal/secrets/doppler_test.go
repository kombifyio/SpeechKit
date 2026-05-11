package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDopplerExecutablePathAllowsOnlyDopplerBinaryNames(t *testing.T) {
	for _, path := range []string{"doppler", "doppler.exe", filepath.Join("C:\\", "Tools", "doppler.exe")} {
		if err := validateDopplerExecutablePath(path); err != nil {
			t.Fatalf("validateDopplerExecutablePath(%q): %v", path, err)
		}
	}
	if err := validateDopplerExecutablePath("powershell.exe"); err == nil {
		t.Fatal("expected non-doppler executable to be rejected")
	}
}

func TestDefaultDopplerSecretLookupRejectsNonDopplerBinary(t *testing.T) {
	if _, err := DefaultDopplerSecretLookup("powershell.exe", "HF_TOKEN", "project", "prd"); err == nil {
		t.Fatal("expected non-doppler executable to be rejected")
	}
}

func TestFindDopplerExecutableUsesExistingEnvOverrideBeforeLookPath(t *testing.T) {
	dopplerPath := filepath.Join(t.TempDir(), "doppler.exe")
	if err := os.WriteFile(dopplerPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write doppler fixture: %v", err)
	}
	t.Setenv("DOPPLER_PATH", dopplerPath)

	lookPathCalls := 0
	got := FindDopplerExecutable(func(string) (string, error) {
		lookPathCalls++
		return "", errors.New("not found")
	})

	if got != dopplerPath {
		t.Fatalf("FindDopplerExecutable = %q, want %q", got, dopplerPath)
	}
	if lookPathCalls != 0 {
		t.Fatalf("lookPath calls = %d, want 0", lookPathCalls)
	}
}

func TestFindDopplerExecutableFallsBackToLookPathAndKnownInstallDirs(t *testing.T) {
	t.Run("look path", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "doppler.exe")
		got := FindDopplerExecutable(func(name string) (string, error) {
			if name != "doppler" {
				t.Fatalf("lookPath name = %q, want doppler", name)
			}
			return want, nil
		})
		if got != want {
			t.Fatalf("FindDopplerExecutable = %q, want %q", got, want)
		}
	})

	t.Run("known install dir", func(t *testing.T) {
		localAppData := t.TempDir()
		want := filepath.Join(localAppData, "Microsoft", "WinGet", "Links", "doppler.exe")
		if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
			t.Fatalf("create fixture dir: %v", err)
		}
		if err := os.WriteFile(want, []byte("placeholder"), 0o600); err != nil {
			t.Fatalf("write doppler fixture: %v", err)
		}
		t.Setenv("LOCALAPPDATA", localAppData)
		t.Setenv("USERPROFILE", t.TempDir())
		t.Setenv("ProgramFiles", t.TempDir())
		t.Setenv("ProgramFiles(x86)", t.TempDir())

		got := FindDopplerExecutable(func(string) (string, error) {
			return "", errors.New("not found")
		})
		if got != want {
			t.Fatalf("FindDopplerExecutable = %q, want %q", got, want)
		}
	})
}

package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/secrets"
)

func TestResolveSecret_EnvVar(t *testing.T) {
	secretsRestore := secrets.UseMemoryStoreForTests()
	t.Cleanup(secretsRestore)
	t.Setenv("TEST_SECRET_KEY", "test-value-123")
	val := ResolveSecret("TEST_SECRET_KEY")
	assertCredentialFixture(t, "environment secret", val, "test-value-123")
}

func TestResolveSecret_DopplerFallback(t *testing.T) {
	secretsRestore := secrets.UseMemoryStoreForTests()
	t.Cleanup(secretsRestore)
	t.Cleanup(resetDopplerHooksForTests)
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")
	dopplerLookPath = func(file string) (string, error) {
		if file != "doppler" {
			t.Fatalf("lookPath file = %q", file)
		}
		return "C:\\fake\\doppler.exe", nil
	}
	dopplerSecretLookup = func(dopplerPath, key, project, cfg string) (string, error) {
		if dopplerPath != "C:\\fake\\doppler.exe" {
			t.Fatalf("dopplerPath = %q", dopplerPath)
		}
		if key != "TEST_DOPPLER_SECRET" {
			t.Fatalf("key = %q", key)
		}
		if project == "test-project" && cfg == "stage" {
			return "secret-from-doppler", nil
		}
		return "", errors.New("not found")
	}

	value := ResolveSecret("TEST_DOPPLER_SECRET")
	assertCredentialFixture(t, "Doppler secret", value, "secret-from-doppler")
}

func TestFindDopplerExecutableUsesEnvOverride(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	fake := filepath.Join(t.TempDir(), "doppler.exe")
	if err := os.WriteFile(fake, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOPPLER_PATH", fake)

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestFindDopplerExecutableFallsBackToWingetLink(t *testing.T) {
	t.Cleanup(resetDopplerHooksForTests)
	dopplerLookPath = func(string) (string, error) {
		return "", &exec.Error{Name: "doppler", Err: errors.New("not found")}
	}

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	fake := filepath.Join(localAppData, "Microsoft", "WinGet", "Links", "doppler.exe")
	if err := os.MkdirAll(filepath.Dir(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := findDopplerExecutable()

	if path != fake {
		t.Fatalf("findDopplerExecutable = %q, want %q", path, fake)
	}
}

func TestDopplerProjectsAndConfigsRequireExplicitEnv(t *testing.T) {
	t.Setenv("DOPPLER_PROJECT", "test-project")
	t.Setenv("DOPPLER_CONFIG", "stage")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) == 0 || projects[0] != "test-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) == 0 || configs[0] != "stage" {
		t.Fatalf("configs = %v", configs)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsFallBackToManagedDefaults(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = "managed-project"
	managedDopplerDefaultConfig = "prd"
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})
	unsetEnvForTest(t, "DOPPLER_PROJECT")
	unsetEnvForTest(t, "DOPPLER_CONFIG")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 1 || projects[0] != "managed-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 || configs[0] != "prd" {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsPreferExplicitEnvOverManagedDefaults(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = "managed-project"
	managedDopplerDefaultConfig = "prd"
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})

	t.Setenv("DOPPLER_PROJECT", "dev-project")
	t.Setenv("DOPPLER_CONFIG", "dev")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 1 || projects[0] != "dev-project" {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 1 || configs[0] != "dev" {
		t.Fatalf("configs = %v", configs)
	}
}

func TestDopplerProjectsAndConfigsStayEmptyWithoutEnv(t *testing.T) {
	previousProject := managedDopplerDefaultProject
	previousConfig := managedDopplerDefaultConfig
	managedDopplerDefaultProject = ""
	managedDopplerDefaultConfig = ""
	t.Cleanup(func() {
		managedDopplerDefaultProject = previousProject
		managedDopplerDefaultConfig = previousConfig
	})
	t.Setenv("DOPPLER_PROJECT", "")
	t.Setenv("DOPPLER_CONFIG", "")

	projects := dopplerProjects()
	configs := dopplerConfigs()

	if len(projects) != 0 {
		t.Fatalf("projects = %v", projects)
	}
	if len(configs) != 0 {
		t.Fatalf("configs = %v", configs)
	}
}

//go:build !darwin

package runtimepath

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

func installedLayout(t *testing.T) (roaming, local string) {
	t.Helper()
	roaming, local = t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", roaming)
	t.Setenv("LOCALAPPDATA", local)
	withMocks(t, mockExecutable(filepath.Join(t.TempDir(), "app.exe"), nil), mockStat(map[string]bool{}), nil)
	return roaming, local
}

// A root that registered no product identity gets no path at all, never a
// relative one or another product's.
func TestPathsWithoutProductIdentityFailClosed(t *testing.T) {
	installedLayout(t)
	productid.ClearForTest(t)
	for name, path := range map[string]func() (string, error){
		"DataDir": DataDir, "LocalDataDir": LocalDataDir, "ContentDir": ContentDir,
		"ModelsDir": ModelsDir, "ConfigFilePath": ConfigFilePath, "SecretsDir": SecretsDir,
	} {
		if got, err := path(); !errors.Is(err, productid.ErrNoIdentity) || got != "" {
			t.Errorf("%s without identity = %q, %v; want ErrNoIdentity", name, got, err)
		}
	}
}

// The reference client keeps the data directories every earlier release
// used, so an upgrade finds its config, content and models where they are.
func TestReferenceKeepsTheExistingDataDirectories(t *testing.T) {
	roaming, local := installedLayout(t)
	productid.UseForTest(t, productid.Reference)
	if got := must(t, DataDir); got != filepath.Join(roaming, "SpeechKit") {
		t.Errorf("DataDir = %q", got)
	}
	if got := must(t, LocalDataDir); got != filepath.Join(local, "SpeechKit") {
		t.Errorf("LocalDataDir = %q", got)
	}
}

// Two products on one machine never share a data directory.
func TestProductsNeverShareADataDirectory(t *testing.T) {
	installedLayout(t)
	productid.UseForTest(t, productid.Reference)
	reference := must(t, DataDir)
	other := productid.Reference
	other.Vendor, other.Product, other.DataDirName = "Contoso", "Notes", "Contoso Notes"
	productid.UseForTest(t, other)
	if got := must(t, DataDir); got == reference {
		t.Fatalf("two products share the data directory %q", got)
	}
}

// The portable switch is <EnvPrefix>_DISABLE_PORTABLE: the reference keeps
// SPEECHKIT_DISABLE_PORTABLE, another product reads its own prefix only.
func TestPortableSwitchFollowsTheEnvironmentPrefix(t *testing.T) {
	t.Setenv("SPEECHKIT_DISABLE_PORTABLE", "1")
	productid.UseForTest(t, productid.Reference)
	if !portableDisabled() {
		t.Error("reference ignores SPEECHKIT_DISABLE_PORTABLE")
	}
	other := productid.Reference
	other.EnvPrefix = "NOTES"
	productid.UseForTest(t, other)
	if portableDisabled() {
		t.Error("another product reads SPEECHKIT_DISABLE_PORTABLE")
	}
	t.Setenv("NOTES_DISABLE_PORTABLE", "1")
	if !portableDisabled() {
		t.Error("another product ignores its own switch")
	}
}

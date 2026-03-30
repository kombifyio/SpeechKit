//go:build windows

package secrets

import "testing"

func TestMigrateInstallTokenBootstrapStoresInstallTokenAndClearsBootstrap(t *testing.T) {
	restoreStore := UseMemoryStoreForTests()
	defer restoreStore()

	prevRead := readBootstrapInstallToken
	prevClear := clearBootstrapInstallToken
	defer func() {
		readBootstrapInstallToken = prevRead
		clearBootstrapInstallToken = prevClear
	}()

	cleared := false
	readBootstrapInstallToken = func() (string, error) {
		return "install-token", nil
	}
	clearBootstrapInstallToken = func() error {
		cleared = true
		return nil
	}

	migrated, err := MigrateInstallTokenBootstrap()
	if err != nil {
		t.Fatalf("migrate bootstrap: %v", err)
	}
	if !migrated {
		t.Fatal("expected bootstrap token migration")
	}
	if !cleared {
		t.Fatal("expected bootstrap token to be cleared")
	}

	token, status, err := ResolveHuggingFaceToken(func() string { return "" })
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "install-token" {
		t.Fatalf("token = %q", token)
	}
	if status.ActiveSource != TokenSourceInstall {
		t.Fatalf("active source = %q", status.ActiveSource)
	}
}

func TestMigrateInstallTokenBootstrapSkipsEmptyValue(t *testing.T) {
	restoreStore := UseMemoryStoreForTests()
	defer restoreStore()

	prevRead := readBootstrapInstallToken
	prevClear := clearBootstrapInstallToken
	defer func() {
		readBootstrapInstallToken = prevRead
		clearBootstrapInstallToken = prevClear
	}()

	cleared := false
	readBootstrapInstallToken = func() (string, error) {
		return "   ", nil
	}
	clearBootstrapInstallToken = func() error {
		cleared = true
		return nil
	}

	migrated, err := MigrateInstallTokenBootstrap()
	if err != nil {
		t.Fatalf("migrate bootstrap: %v", err)
	}
	if migrated {
		t.Fatal("did not expect migration for empty bootstrap token")
	}
	if cleared {
		t.Fatal("did not expect bootstrap clear for empty token")
	}
}

package secrets

import "testing"

func TestResolveHuggingFaceTokenPrefersUserToken(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetInstallHuggingFaceToken("install-token"); err != nil {
		t.Fatalf("set install token: %v", err)
	}
	if err := SetUserHuggingFaceToken("user-token"); err != nil {
		t.Fatalf("set user token: %v", err)
	}

	token, status, err := ResolveHuggingFaceToken(func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "user-token" {
		t.Fatalf("token = %q", token)
	}
	if status.ActiveSource != TokenSourceUser {
		t.Fatalf("active source = %q", status.ActiveSource)
	}
	if !status.HasUserToken {
		t.Fatal("expected user token status")
	}
	if !status.HasInstallToken {
		t.Fatal("expected install token status")
	}
}

func TestResolveHuggingFaceTokenFallsBackToInstallToken(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetInstallHuggingFaceToken("install-token"); err != nil {
		t.Fatalf("set install token: %v", err)
	}

	token, status, err := ResolveHuggingFaceToken(func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "install-token" {
		t.Fatalf("token = %q", token)
	}
	if status.ActiveSource != TokenSourceInstall {
		t.Fatalf("active source = %q", status.ActiveSource)
	}
	if status.HasUserToken {
		t.Fatal("did not expect user token status")
	}
	if !status.HasInstallToken {
		t.Fatal("expected install token status")
	}
}

func TestResolveHuggingFaceTokenFallsBackToEnvResolver(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	token, status, err := ResolveHuggingFaceToken(func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q", token)
	}
	if status.ActiveSource != TokenSourceEnv {
		t.Fatalf("active source = %q", status.ActiveSource)
	}
	if status.HasUserToken {
		t.Fatal("did not expect user token status")
	}
	if status.HasInstallToken {
		t.Fatal("did not expect install token status")
	}
}

func TestClearUserHuggingFaceTokenFallsBackToInstallToken(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetInstallHuggingFaceToken("install-token"); err != nil {
		t.Fatalf("set install token: %v", err)
	}
	if err := SetUserHuggingFaceToken("user-token"); err != nil {
		t.Fatalf("set user token: %v", err)
	}
	if err := ClearUserHuggingFaceToken(); err != nil {
		t.Fatalf("clear user token: %v", err)
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
	if status.HasUserToken {
		t.Fatal("did not expect user token after clear")
	}
}

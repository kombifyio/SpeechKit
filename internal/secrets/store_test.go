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

func TestResolveNamedSecretPrefersStoredSecret(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetNamedSecret("openai", " stored-token "); err != nil {
		t.Fatalf("set named secret: %v", err)
	}

	token, status, err := ResolveNamedSecret("openai", func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve named secret: %v", err)
	}
	if token != "stored-token" {
		t.Fatalf("token = %q, want stored-token", token)
	}
	if status.ActiveSource != TokenSourceUser || !status.HasUserToken {
		t.Fatalf("status = %+v, want stored user source", status)
	}
}

func TestClearNamedSecretFallsBackToEnvResolver(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetNamedSecret("google", "stored-token"); err != nil {
		t.Fatalf("set named secret: %v", err)
	}
	if err := ClearNamedSecret("google"); err != nil {
		t.Fatalf("clear named secret: %v", err)
	}

	token, status, err := ResolveNamedSecret("google", func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve named secret: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q, want env-token", token)
	}
	if status.ActiveSource != TokenSourceEnv || status.HasUserToken {
		t.Fatalf("status = %+v, want env source without stored token", status)
	}
}

func TestSetNamedSecretRejectsEmptyValue(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetNamedSecret("empty", " \t "); err == nil {
		t.Fatal("SetNamedSecret accepted an empty value")
	}
}

func TestNamedSecretStatusSkipsBlankName(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	status, err := NamedSecretStatus("  ", func() string { return "env-token" })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.ActiveSource != TokenSourceNone {
		t.Fatalf("status = %+v, want none", status)
	}
}

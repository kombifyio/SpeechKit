package secrets

import (
	"strings"
	"testing"
)

func TestSetTokensRejectEmptyValues(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	for name, set := range map[string]func(string) error{
		"user":    SetUserHuggingFaceToken,
		"install": SetInstallHuggingFaceToken,
		"named":   func(value string) error { return SetNamedSecret("api-key", value) },
	} {
		t.Run(name, func(t *testing.T) {
			err := set(" \t\n ")
			if err == nil {
				t.Fatal("expected empty secret to be rejected")
			}
			if !strings.Contains(err.Error(), "cannot be empty") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestResolveNamedSecretPrefersStoredSecretOverEnv(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetNamedSecret(" openai ", " stored-token "); err != nil {
		t.Fatalf("set named secret: %v", err)
	}

	value, status, err := ResolveNamedSecret("openai", func() string { return "env-token" })
	if err != nil {
		t.Fatalf("resolve named secret: %v", err)
	}
	if value != "stored-token" {
		t.Fatalf("value = %q", value)
	}
	if status.ActiveSource != TokenSourceUser || !status.HasUserToken {
		t.Fatalf("status = %+v", status)
	}
}

func TestResolveNamedSecretFallsBackToEnvAfterClear(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := SetNamedSecret("openai", "stored-token"); err != nil {
		t.Fatalf("set named secret: %v", err)
	}
	if err := ClearNamedSecret("openai"); err != nil {
		t.Fatalf("clear named secret: %v", err)
	}

	value, status, err := ResolveNamedSecret("openai", func() string { return " env-token " })
	if err != nil {
		t.Fatalf("resolve named secret: %v", err)
	}
	if value != "env-token" {
		t.Fatalf("value = %q", value)
	}
	if status.ActiveSource != TokenSourceEnv || status.HasUserToken {
		t.Fatalf("status = %+v", status)
	}
}

func TestNamedSecretStatusEmptyNameDoesNotTouchEnvResolver(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	envCalls := 0
	status, err := NamedSecretStatus(" \t ", func() string {
		envCalls++
		return "env-token"
	})
	if err != nil {
		t.Fatalf("named secret status: %v", err)
	}
	if status.ActiveSource != TokenSourceNone {
		t.Fatalf("status = %+v", status)
	}
	if envCalls != 0 {
		t.Fatalf("env resolver calls = %d, want 0", envCalls)
	}
}

func TestHuggingFaceStatusTreatsWhitespaceStoredTokensAsAbsent(t *testing.T) {
	restore := UseMemoryStoreForTests()
	defer restore()

	if err := currentStore().Store(huggingFaceUserKey, "   "); err != nil {
		t.Fatalf("store whitespace user token: %v", err)
	}
	if err := currentStore().Store(huggingFaceInstallKey, "\n\t"); err != nil {
		t.Fatalf("store whitespace install token: %v", err)
	}

	value, status, err := ResolveHuggingFaceToken(func() string { return " env-token " })
	if err != nil {
		t.Fatalf("resolve huggingface token: %v", err)
	}
	if value != "env-token" {
		t.Fatalf("value = %q", value)
	}
	if status.HasUserToken || status.HasInstallToken || status.ActiveSource != TokenSourceEnv {
		t.Fatalf("status = %+v", status)
	}
}

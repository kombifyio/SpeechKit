package storage

import (
	"context"
	"errors"
	"testing"
)

func TestNormalizeScopeDefaultsToLocalWithoutRequiringUserOrTenant(t *testing.T) {
	scope := NormalizeScope(Scope{})

	if scope.InstallID != LocalInstallID {
		t.Fatalf("InstallID = %q, want %q", scope.InstallID, LocalInstallID)
	}
	if scope.UserID != "" || scope.TenantID != "" {
		t.Fatalf("local scope unexpectedly requires user/tenant: %+v", scope)
	}
	if scope.Key() != "install:local" {
		t.Fatalf("Key() = %q, want install:local", scope.Key())
	}
	if err := ScopeOptional.Validate(scope); err != nil {
		t.Fatalf("ScopeOptional rejected local scope: %v", err)
	}
}

func TestScopeContextRoundTripNormalizesScope(t *testing.T) {
	ctx := WithScope(context.Background(), Scope{
		InstallID: " desktop ",
		DeviceID:  " laptop ",
		Labels: map[string]string{
			" env ": " dev ",
			"":      "ignored",
		},
	})

	scope := ScopeFromContext(ctx)
	if scope.InstallID != "desktop" || scope.DeviceID != "laptop" {
		t.Fatalf("scope not normalized from context: %+v", scope)
	}
	if got := scope.Labels["env"]; got != "dev" {
		t.Fatalf("label env = %q, want dev", got)
	}
}

func TestScopeKeyEscapesSeparators(t *testing.T) {
	withSeparator := NormalizeScope(Scope{UserID: "alice|device:x"})
	separateFields := NormalizeScope(Scope{UserID: "alice", DeviceID: "x"})

	if withSeparator.Key() == separateFields.Key() {
		t.Fatalf("scope keys collide: %q", withSeparator.Key())
	}
	if got := withSeparator.Key(); got != "user:alice%7Cdevice%3Ax" {
		t.Fatalf("escaped key = %q, want user:alice%%7Cdevice%%3Ax", got)
	}
}

func TestScopePoliciesValidateOnlyWhenBackendsRequireIdentity(t *testing.T) {
	local := NormalizeScope(Scope{})
	user := NormalizeScope(Scope{UserID: "u_123"})
	tenant := NormalizeScope(Scope{TenantID: "t_123"})

	if err := ScopeOptional.Validate(local); err != nil {
		t.Fatalf("ScopeOptional rejected local scope: %v", err)
	}
	if err := ScopeUserRequired.Validate(local); !errors.Is(err, ErrScopeUserRequired) {
		t.Fatalf("ScopeUserRequired error = %v, want ErrScopeUserRequired", err)
	}
	if err := ScopeUserRequired.Validate(user); err != nil {
		t.Fatalf("ScopeUserRequired rejected user scope: %v", err)
	}
	if err := ScopeTenantRequired.Validate(local); !errors.Is(err, ErrScopeTenantRequired) {
		t.Fatalf("ScopeTenantRequired error = %v, want ErrScopeTenantRequired", err)
	}
	if err := ScopeTenantRequired.Validate(tenant); err != nil {
		t.Fatalf("ScopeTenantRequired rejected tenant scope: %v", err)
	}
}

package auth

import (
	"context"
	"testing"
)

// mockProvider is a minimal AuthProvider for registry tests.
type mockProvider struct {
	loggedIn bool
}

func (m *mockProvider) StartDeviceCodeFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	return nil, nil
}
func (m *mockProvider) PollDeviceCode(ctx context.Context, deviceCode string) (*TokenPair, error) {
	return nil, nil
}
func (m *mockProvider) GetAccessToken(ctx context.Context) (string, error) { return "", nil }
func (m *mockProvider) GetIdentity(ctx context.Context) (*Identity, error)  { return nil, nil }
func (m *mockProvider) Logout(ctx context.Context) error                    { return nil }
func (m *mockProvider) IsLoggedIn() bool                                    { return m.loggedIn }

// resetRegistry clears the registered provider between tests.
func resetRegistry() {
	registryMu.Lock()
	registeredProvider = nil
	registryMu.Unlock()
}

func TestGetAuthProvider_DefaultNil(t *testing.T) {
	resetRegistry()

	if p := GetAuthProvider(); p != nil {
		t.Fatalf("expected nil provider, got %v", p)
	}
}

func TestHasAuthProvider_DefaultFalse(t *testing.T) {
	resetRegistry()

	if HasAuthProvider() {
		t.Fatal("expected HasAuthProvider() == false with no provider registered")
	}
}

func TestRegisterAndGetAuthProvider(t *testing.T) {
	resetRegistry()
	defer resetRegistry()

	mock := &mockProvider{}
	RegisterAuthProvider(mock)

	got := GetAuthProvider()
	if got != mock {
		t.Fatalf("expected registered mock provider, got %v", got)
	}
}

func TestHasAuthProvider_AfterRegister(t *testing.T) {
	resetRegistry()
	defer resetRegistry()

	RegisterAuthProvider(&mockProvider{})

	if !HasAuthProvider() {
		t.Fatal("expected HasAuthProvider() == true after registration")
	}
}

func TestRegisterAuthProvider_Overwrite(t *testing.T) {
	resetRegistry()
	defer resetRegistry()

	first := &mockProvider{loggedIn: false}
	second := &mockProvider{loggedIn: true}

	RegisterAuthProvider(first)
	RegisterAuthProvider(second)

	got := GetAuthProvider()
	if got != second {
		t.Fatal("expected second provider to overwrite first")
	}
	if !got.IsLoggedIn() {
		t.Fatal("expected second provider (loggedIn=true)")
	}
}

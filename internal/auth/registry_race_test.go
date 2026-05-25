package auth

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Audit 4.5 — exercise the registry RWMutex under concurrent
// Register/Get/HasAuthProvider/Logout. The current auth_test.go pins
// the happy-path API; this file is the race-detector contract that
// downstream products can rely on when they import the registry from
// init() in one goroutine and have request handlers call GetAuthProvider()
// from many goroutines simultaneously.

type fakeProvider struct {
	id Identity
}

func (f *fakeProvider) StartDeviceCodeFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	return &DeviceCodeResponse{DeviceCode: "dc", UserCode: "uc"}, nil
}
func (f *fakeProvider) PollDeviceCode(ctx context.Context, _ string) (*TokenPair, error) {
	return &TokenPair{AccessToken: "tok", UserID: f.id.UserID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (f *fakeProvider) GetAccessToken(ctx context.Context) (string, error) { return "tok", nil }
func (f *fakeProvider) GetIdentity(ctx context.Context) (*Identity, error) {
	id := f.id
	return &id, nil
}
func (f *fakeProvider) Logout(ctx context.Context) error { return nil }
func (f *fakeProvider) IsLoggedIn() bool                 { return f.id.UserID != "" }

func TestRegistry_ConcurrentRegisterAndRead(t *testing.T) {
	// Reset state at end to avoid leaking the fake into other tests
	// in the same go-test run.
	t.Cleanup(func() { RegisterAuthProvider(nil) })

	p := &fakeProvider{id: Identity{UserID: "concurrent", OrgID: "test"}}

	var wg sync.WaitGroup
	const readers = 8
	const iterations = 200

	// Writer: flips the registry between (p) and (nil) repeatedly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				RegisterAuthProvider(p)
			} else {
				RegisterAuthProvider(nil)
			}
		}
	}()

	// Readers: hammer Get + Has + (if set) GetIdentity.
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				provider := GetAuthProvider()
				_ = HasAuthProvider()
				if provider != nil {
					id, err := provider.GetIdentity(context.Background())
					if err == nil && id != nil && id.UserID != "" && id.UserID != "concurrent" {
						t.Errorf("GetIdentity returned unexpected user %q", id.UserID)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

func TestRegistry_NilRegistrationClearsProvider(t *testing.T) {
	t.Cleanup(func() { RegisterAuthProvider(nil) })

	RegisterAuthProvider(&fakeProvider{id: Identity{UserID: "x"}})
	if !HasAuthProvider() {
		t.Fatal("HasAuthProvider returned false after Register")
	}
	RegisterAuthProvider(nil)
	if HasAuthProvider() {
		t.Fatal("HasAuthProvider returned true after nil-register")
	}
	if GetAuthProvider() != nil {
		t.Fatal("GetAuthProvider returned non-nil after nil-register")
	}
}

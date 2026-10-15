package netsec

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func loopbackOnlyPolicy(allowSetup bool) Policy {
	return Policy{
		Validation:        &ValidationOptions{AllowLoopback: true, AllowHTTP: true, RequireLocal: true},
		AllowSetupTraffic: allowSetup,
	}
}

// A policy installed after a client was built still binds it: a public
// address is refused before any socket opens, loopback keeps working.
func TestConfiguredPolicyRefusesPublicDialForExistingClients(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	client := NewClient(ClientOptions{})

	Configure(loopbackOnlyPolicy(false))
	t.Cleanup(func() { Configure(Policy{}) })

	if resp, err := client.Get(srv.URL); err != nil {
		t.Fatalf("loopback request under policy: %v", err)
	} else {
		_ = resp.Body.Close()
	}
	if _, err := client.Get("http://8.8.8.8/"); !errors.Is(err, ErrPublicBlocked) {
		t.Fatalf("public request err = %v, want ErrPublicBlocked", err)
	}
	if _, err := DialContext(context.Background(), "tcp", "8.8.8.8:443"); !errors.Is(err, ErrPublicBlocked) {
		t.Fatalf("raw public dial err = %v, want ErrPublicBlocked", err)
	}
}

// Setup-traffic clients are bound by the policy until the user opts in.
func TestSetupTrafficClientFollowsOptIn(t *testing.T) {
	setup := NewClient(ClientOptions{SetupTraffic: true})
	t.Cleanup(func() { Configure(Policy{}) })

	Configure(loopbackOnlyPolicy(false))
	if _, err := setup.Get("http://8.8.8.8/"); !errors.Is(err, ErrPublicBlocked) {
		t.Fatalf("without opt-in err = %v, want ErrPublicBlocked", err)
	}

	Configure(loopbackOnlyPolicy(true))
	_, err := setup.Get("http://8.8.8.8:1/")
	if errors.Is(err, ErrPublicBlocked) {
		t.Fatalf("with opt-in the policy still refused: %v", err)
	}
}

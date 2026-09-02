package speechkit_test

import (
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func TestParseNetworkScope(t *testing.T) {
	cases := []struct {
		raw     string
		want    speechkit.NetworkScope
		wantErr bool
	}{
		{"", speechkit.NetworkScopeOpen, false},
		{"open", speechkit.NetworkScopeOpen, false},
		{"  Open  ", speechkit.NetworkScopeOpen, false},
		{"local_network", speechkit.NetworkScopeLocalNetwork, false},
		{"LOCAL_NETWORK", speechkit.NetworkScopeLocalNetwork, false},
		{"device_only", speechkit.NetworkScopeDeviceOnly, false},
		{"lan", "", true},
		{"offline", "", true},
		{"local-network", "", true},
	}
	for _, tc := range cases {
		got, err := speechkit.ParseNetworkScope(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, speechkit.ErrUnknownNetworkScope) {
				t.Errorf("ParseNetworkScope(%q) err = %v, want ErrUnknownNetworkScope", tc.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseNetworkScope(%q) unexpected error: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseNetworkScope(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestNormalizeNetworkScopeFailsClosed(t *testing.T) {
	if got := speechkit.NormalizeNetworkScope("no-such-scope"); got != speechkit.NetworkScopeDeviceOnly {
		t.Fatalf("NormalizeNetworkScope(unknown) = %q, want device_only", got)
	}
	if got := speechkit.NormalizeNetworkScope(""); got != speechkit.NetworkScopeOpen {
		t.Fatalf("NormalizeNetworkScope(empty) = %q, want open", got)
	}
}

// TestNetworkScopeClassifiesEntireCatalog pins the behavioural contract for
// every shipped profile: open allows everything, local_network allows only
// on-device and local-service providers, device_only allows only providers
// the app manages itself. A new catalog entry without a ProviderKind (or with
// a new kind) must make an explicit appearance here.
func TestNetworkScopeClassifiesEntireCatalog(t *testing.T) {
	profiles := catalog.DefaultProviderProfiles()
	if len(profiles) == 0 {
		t.Fatal("catalog is empty")
	}
	for _, p := range profiles {
		if allowed, reason := speechkit.NetworkScopeOpen.AllowsProfile(p); !allowed || reason != "" {
			t.Errorf("open scope blocks %s (%s)", p.ID, reason)
		}

		lnAllowed, lnReason := speechkit.NetworkScopeLocalNetwork.AllowsProfile(p)
		doAllowed, doReason := speechkit.NetworkScopeDeviceOnly.AllowsProfile(p)

		switch p.ProviderKind {
		case speechkit.ProviderKindLocalBuiltIn:
			if !lnAllowed || !doAllowed {
				t.Errorf("local built-in %s must be allowed everywhere (local_network=%v device_only=%v)", p.ID, lnAllowed, doAllowed)
			}
		case speechkit.ProviderKindLocalProvider:
			if !lnAllowed {
				t.Errorf("local provider %s must be allowed in local_network (%s)", p.ID, lnReason)
			}
			if doAllowed {
				t.Errorf("local provider %s must be blocked in device_only", p.ID)
			}
			if doReason != speechkit.NetworkScopeReasonLocalService {
				t.Errorf("local provider %s device_only reason = %q, want %q", p.ID, doReason, speechkit.NetworkScopeReasonLocalService)
			}
		case speechkit.ProviderKindCloudProvider, speechkit.ProviderKindDirectProvider:
			if lnAllowed || doAllowed {
				t.Errorf("cloud/direct provider %s must be blocked in restricted scopes (local_network=%v device_only=%v)", p.ID, lnAllowed, doAllowed)
			}
			if lnReason != speechkit.NetworkScopeReasonCloudProvider {
				t.Errorf("cloud/direct provider %s local_network reason = %q, want %q", p.ID, lnReason, speechkit.NetworkScopeReasonCloudProvider)
			}
		default:
			t.Errorf("profile %s has unclassified ProviderKind %q — extend NetworkScope.AllowsProviderKind and this test", p.ID, p.ProviderKind)
		}
	}
}

func TestNetworkScopeEndpointPolicy(t *testing.T) {
	// open: everything allowed, nothing extra enforced.
	for _, class := range []speechkit.EndpointClass{speechkit.EndpointClassManagedLoopback, speechkit.EndpointClassLocalService, speechkit.EndpointClassTelemetry, speechkit.EndpointClassCloud} {
		p := speechkit.NetworkScopeOpen.EndpointPolicyFor(class)
		if !p.Allowed || p.Enforce {
			t.Errorf("open %s: got %+v, want allowed without enforcement", class, p)
		}
	}

	// local_network: local classes enforced as local-only; cloud blocked.
	ln := speechkit.NetworkScopeLocalNetwork
	if p := ln.EndpointPolicyFor(speechkit.EndpointClassLocalService); !p.Allowed || !p.Enforce || !p.Validation.RequireLocal {
		t.Errorf("local_network local_service policy = %+v, want enforced RequireLocal", p)
	}
	if p := ln.EndpointPolicyFor(speechkit.EndpointClassCloud); p.Allowed {
		t.Errorf("local_network cloud must be blocked, got %+v", p)
	}
	if p := ln.EndpointPolicyFor(speechkit.EndpointClassManagedLoopback); !p.Allowed || !p.Enforce || p.Validation.AllowPrivate {
		t.Errorf("local_network managed_loopback policy = %+v, want loopback-only enforcement", p)
	}

	// device_only: only managed loopback survives.
	do := speechkit.NetworkScopeDeviceOnly
	if p := do.EndpointPolicyFor(speechkit.EndpointClassManagedLoopback); !p.Allowed || !p.Enforce || p.Validation.AllowPrivate {
		t.Errorf("device_only managed_loopback policy = %+v, want loopback-only enforcement", p)
	}
	for _, class := range []speechkit.EndpointClass{speechkit.EndpointClassLocalService, speechkit.EndpointClassTelemetry, speechkit.EndpointClassCloud} {
		if p := do.EndpointPolicyFor(class); p.Allowed {
			t.Errorf("device_only %s must be blocked, got %+v", class, p)
		}
		if p := do.EndpointPolicyFor(class); p.ReasonID == "" {
			t.Errorf("device_only %s must carry a disabled reason ID", class)
		}
	}

	// Unknown scope fails closed to device_only behaviour.
	if p := speechkit.NetworkScope("bogus").EndpointPolicyFor(speechkit.EndpointClassLocalService); p.Allowed {
		t.Errorf("unknown scope must fail closed, got %+v", p)
	}
}

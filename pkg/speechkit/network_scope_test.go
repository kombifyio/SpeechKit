package speechkit

import (
	"errors"
	"testing"
)

func TestParseNetworkScope(t *testing.T) {
	cases := []struct {
		raw     string
		want    NetworkScope
		wantErr bool
	}{
		{"", NetworkScopeOpen, false},
		{"open", NetworkScopeOpen, false},
		{"  Open  ", NetworkScopeOpen, false},
		{"local_network", NetworkScopeLocalNetwork, false},
		{"LOCAL_NETWORK", NetworkScopeLocalNetwork, false},
		{"device_only", NetworkScopeDeviceOnly, false},
		{"lan", "", true},
		{"offline", "", true},
		{"local-network", "", true},
	}
	for _, tc := range cases {
		got, err := ParseNetworkScope(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, ErrUnknownNetworkScope) {
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
	if got := NormalizeNetworkScope("no-such-scope"); got != NetworkScopeDeviceOnly {
		t.Fatalf("NormalizeNetworkScope(unknown) = %q, want device_only", got)
	}
	if got := NormalizeNetworkScope(""); got != NetworkScopeOpen {
		t.Fatalf("NormalizeNetworkScope(empty) = %q, want open", got)
	}
}

// TestNetworkScopeClassifiesEntireCatalog pins the behavioural contract for
// every shipped profile: open allows everything, local_network allows only
// on-device and local-service providers, device_only allows only providers
// the app manages itself. A new catalog entry without a ProviderKind (or with
// a new kind) must make an explicit appearance here.
func TestNetworkScopeClassifiesEntireCatalog(t *testing.T) {
	profiles := DefaultProviderProfiles()
	if len(profiles) == 0 {
		t.Fatal("catalog is empty")
	}
	for _, p := range profiles {
		if allowed, reason := NetworkScopeOpen.AllowsProfile(p); !allowed || reason != "" {
			t.Errorf("open scope blocks %s (%s)", p.ID, reason)
		}

		lnAllowed, lnReason := NetworkScopeLocalNetwork.AllowsProfile(p)
		doAllowed, doReason := NetworkScopeDeviceOnly.AllowsProfile(p)

		switch p.ProviderKind {
		case ProviderKindLocalBuiltIn:
			if !lnAllowed || !doAllowed {
				t.Errorf("local built-in %s must be allowed everywhere (local_network=%v device_only=%v)", p.ID, lnAllowed, doAllowed)
			}
		case ProviderKindLocalProvider:
			if !lnAllowed {
				t.Errorf("local provider %s must be allowed in local_network (%s)", p.ID, lnReason)
			}
			if doAllowed {
				t.Errorf("local provider %s must be blocked in device_only", p.ID)
			}
			if doReason != NetworkScopeReasonLocalService {
				t.Errorf("local provider %s device_only reason = %q, want %q", p.ID, doReason, NetworkScopeReasonLocalService)
			}
		case ProviderKindCloudProvider, ProviderKindDirectProvider:
			if lnAllowed || doAllowed {
				t.Errorf("cloud/direct provider %s must be blocked in restricted scopes (local_network=%v device_only=%v)", p.ID, lnAllowed, doAllowed)
			}
			if lnReason != NetworkScopeReasonCloudProvider {
				t.Errorf("cloud/direct provider %s local_network reason = %q, want %q", p.ID, lnReason, NetworkScopeReasonCloudProvider)
			}
		default:
			t.Errorf("profile %s has unclassified ProviderKind %q — extend NetworkScope.AllowsProviderKind and this test", p.ID, p.ProviderKind)
		}
	}
}

func TestNetworkScopeEndpointPolicy(t *testing.T) {
	// open: everything allowed, nothing extra enforced.
	for _, class := range []EndpointClass{EndpointClassManagedLoopback, EndpointClassLocalService, EndpointClassTelemetry, EndpointClassCloud} {
		p := NetworkScopeOpen.EndpointPolicyFor(class)
		if !p.Allowed || p.Enforce {
			t.Errorf("open %s: got %+v, want allowed without enforcement", class, p)
		}
	}

	// local_network: local classes enforced as local-only; cloud blocked.
	ln := NetworkScopeLocalNetwork
	if p := ln.EndpointPolicyFor(EndpointClassLocalService); !p.Allowed || !p.Enforce || !p.Validation.RequireLocal {
		t.Errorf("local_network local_service policy = %+v, want enforced RequireLocal", p)
	}
	if p := ln.EndpointPolicyFor(EndpointClassCloud); p.Allowed {
		t.Errorf("local_network cloud must be blocked, got %+v", p)
	}
	if p := ln.EndpointPolicyFor(EndpointClassManagedLoopback); !p.Allowed || !p.Enforce || p.Validation.AllowPrivate {
		t.Errorf("local_network managed_loopback policy = %+v, want loopback-only enforcement", p)
	}

	// device_only: only managed loopback survives.
	do := NetworkScopeDeviceOnly
	if p := do.EndpointPolicyFor(EndpointClassManagedLoopback); !p.Allowed || !p.Enforce || p.Validation.AllowPrivate {
		t.Errorf("device_only managed_loopback policy = %+v, want loopback-only enforcement", p)
	}
	for _, class := range []EndpointClass{EndpointClassLocalService, EndpointClassTelemetry, EndpointClassCloud} {
		if p := do.EndpointPolicyFor(class); p.Allowed {
			t.Errorf("device_only %s must be blocked, got %+v", class, p)
		}
		if p := do.EndpointPolicyFor(class); p.ReasonID == "" {
			t.Errorf("device_only %s must carry a disabled reason ID", class)
		}
	}

	// Unknown scope fails closed to device_only behaviour.
	if p := NetworkScope("bogus").EndpointPolicyFor(EndpointClassLocalService); p.Allowed {
		t.Errorf("unknown scope must fail closed, got %+v", p)
	}
}

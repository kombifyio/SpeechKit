package config

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

// HKCU is writable by the user: for the retention TTL it may only shorten
// what the machine pins, never lengthen it and never remove it (0).
func TestUserHiveCanOnlyShortenThePinnedRetentionTTL(t *testing.T) {
	for _, tc := range []struct {
		name string
		user int
		want int
	}{
		{name: "longer", user: 90, want: 30},
		{name: "remove", user: 0, want: 30},
		{name: "shorter", user: 7, want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			cfg.Privacy.RetentionTTLDays = 365

			AttachPolicy(cfg, mergePolicyHives(PolicyValues{PrivacyRetentionTTLDays: ptrInt(30)}, PolicyValues{}, PolicyValues{PrivacyRetentionTTLDays: ptrInt(tc.user)}))

			if cfg.Privacy.RetentionTTLDays != tc.want {
				t.Fatalf("retention TTL = %d days, want %d", cfg.Privacy.RetentionTTLDays, tc.want)
			}
		})
	}
}

// A pinned data root on a network path is rejected by key name and never
// reaches config, so start-up keeps the root it would have used; a local
// absolute path is applied.
func TestPinnedDataRootOnANetworkPathIsRejected(t *testing.T) {
	local := t.TempDir()
	for _, tc := range []struct {
		pin     string
		applied bool
	}{
		{pin: `\\fileserver\speechkit`, applied: false},
		{pin: "relative/dir", applied: false},
		{pin: local, applied: true},
	} {
		managed := t.TempDir()
		writePlist(t, filepath.Join(managed, testBundleID+".plist"), `<key>Storage.DataRoot</key><string>`+tc.pin+`</string>`)
		cfg := defaults()
		policy := readManagedPreferences(managed, testBundleID, nil)

		AttachPolicy(cfg, policy)

		if tc.applied {
			if cfg.Store.DataRoot != tc.pin {
				t.Fatalf("pin %q: data root = %q", tc.pin, cfg.Store.DataRoot)
			}
			continue
		}
		if cfg.Store.DataRoot != "" || !policy.rejectedKey(`Storage\DataRoot`) {
			t.Fatalf("pin %q: data root = %q, rejected = %v", tc.pin, cfg.Store.DataRoot, policy.Rejected)
		}
	}
}

// Load binds the policy before it returns, so the data root pin is already in
// cfg when the desktop resolves its data location from it.
func TestLoadBindsTheDataRootPinBeforeTheDesktopResolvesIt(t *testing.T) {
	root := t.TempDir()
	previous := readPolicyValues
	readPolicyValues = func() PolicyValues {
		return singleSourcePolicy(PolicyValues{StorageDataRoot: root}, PolicySourceMachinePolicies)
	}
	t.Cleanup(func() { readPolicyValues = previous })

	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.DataRoot != root {
		t.Fatalf("data root after Load = %q, want the pinned %q", cfg.Store.DataRoot, root)
	}
}

// A pinned forward-required cannot be switched off by config.toml or by a
// per-user value.
func TestPinnedForwardRequiredCannotBeSwitchedOff(t *testing.T) {
	cfg := defaults()
	cfg.Audit.ForwardRequired = false

	AttachPolicy(cfg, mergePolicyHives(PolicyValues{AuditForwardRequired: ptrBool(true)}, PolicyValues{}, PolicyValues{AuditForwardRequired: ptrBool(false)}))
	cfg.Audit.ForwardRequired = false // a later config write
	EnforcePolicy(cfg)

	if !cfg.Audit.ForwardRequired {
		t.Fatal("forward_required was switched off under a pinned policy")
	}
}

// A refused edit of a pinned value is an error callers branch on with
// errors.Is and audit by key name, without parsing the message.
func TestPinnedEditIsRefusedWithATypedErrorNamingTheKey(t *testing.T) {
	cfg := defaults()
	AttachPolicy(cfg, singleSourcePolicy(PolicyValues{AuditForwardRequired: ptrBool(true)}, PolicySourceMachinePolicies))
	next := *cfg
	next.Audit.ForwardRequired = false

	err := cfg.CheckPolicyEdit(&next)

	if !errors.Is(err, ErrPolicyLocked) {
		t.Fatalf("error %v does not match ErrPolicyLocked", err)
	}
	if keys := PolicyLockedKeys(err); !slices.Equal(keys, []string{"audit.forward_required"}) {
		t.Fatalf("refused keys = %v", keys)
	}
	if PolicyNameForKey("audit.forward_required") != `Audit\ForwardRequired` {
		t.Fatal("the pinning policy of the key is not resolvable")
	}
}

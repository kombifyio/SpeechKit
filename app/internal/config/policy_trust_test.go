package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// HKCU is writable by the user, so a per-user registry value must never
// loosen what the machine policy or the defaults decided, and security values
// are machine-only; it may only restrict further.
func TestUserHiveCannotLoosenMachineOrDefaultSecurityValues(t *testing.T) {
	machine := PolicyValues{ProvidersEnforceLocalOnly: ptrBool(true), AuditEnabled: ptrBool(true)}
	user := PolicyValues{
		ProvidersEnforceLocalOnly: ptrBool(false),                     // lift the machine enforcement
		AuditEnabled:              ptrBool(false),                     // switch the pinned audit trail off
		VoiceAgentAllowCloud:      ptrBool(true),                      // re-allow cloud Voice Agent providers
		UpdateManifestURL:         "https://updates.attacker.example", // redirect the update channel
		AuditOTLPEndpoint:         "https://sink.attacker.example:4317",
		AuditRetentionDays:        ptrInt(1),
		FoundryEntraClientID:      "00000000-0000-0000-0000-00000000beef",
		FoundryProjectEndpoint:    "https://attacker.services.ai.azure.com",
		CopilotCLIPath:            filepath.Join(t.TempDir(), "copilot.exe"),
		CopilotEnabled:            ptrBool(true),
		PrivacyNetworkScope:       NetworkScopeOpen,
		ProvidersAllowed:          []string{"deepgram"}, // a user list without a machine list
		UpdateEnabled:             ptrBool(false),       // restricting: honoured
		AudioSaveAudio:            ptrBool(false),       // restricting: honoured
	}
	want := defaults()
	cfg := defaults()
	cfg.Audit.Enabled = false // config.toml cannot switch a pinned audit trail off either

	AttachPolicy(cfg, mergePolicyHives(machine, PolicyValues{}, user))

	if !cfg.Policy().EnforcesLocalOnly() || cfg.Routing.Strategy != "local-only" {
		t.Fatal("HKCU lifted the machine EnforceLocalOnly policy")
	}
	if !cfg.Audit.Enabled {
		t.Fatal("the pinned audit trail was switched off")
	}
	if cfg.VoiceAgent.Provider != want.VoiceAgent.Provider || cfg.Policy().VoiceAgentAllowCloud != nil {
		t.Fatalf("HKCU changed the Voice Agent provider policy: %q", cfg.VoiceAgent.Provider)
	}
	if cfg.Update.ManifestURL != want.Update.ManifestURL {
		t.Fatalf("HKCU redirected the update manifest to %q", cfg.Update.ManifestURL)
	}
	if cfg.Audit.OTLPEndpoint != want.Audit.OTLPEndpoint || cfg.Audit.RetentionDays != want.Audit.RetentionDays {
		t.Fatalf("HKCU changed the audit sink/retention: %q, %d days", cfg.Audit.OTLPEndpoint, cfg.Audit.RetentionDays)
	}
	if cfg.Providers.Foundry.EntraClientID != want.Providers.Foundry.EntraClientID ||
		cfg.Providers.Foundry.ProjectEndpoint != want.Providers.Foundry.ProjectEndpoint {
		t.Fatalf("HKCU swapped the Foundry registration or endpoint: %q %q", cfg.Providers.Foundry.EntraClientID, cfg.Providers.Foundry.ProjectEndpoint)
	}
	if cfg.Copilot.CLIPath != want.Copilot.CLIPath || cfg.Copilot.Enabled != want.Copilot.Enabled {
		t.Fatalf("HKCU changed the Copilot CLI: %q enabled=%v", cfg.Copilot.CLIPath, cfg.Copilot.Enabled)
	}
	if cfg.Privacy.NetworkScope != want.Privacy.NetworkScope || cfg.Providers.Allowed != nil {
		t.Fatalf("HKCU pinned a machine-only scope or list: %q %v", cfg.Privacy.NetworkScope, cfg.Providers.Allowed)
	}
	if cfg.Update.Enabled || cfg.Store.SaveAudio {
		t.Fatal(`a restricting HKCU value (Update\Enabled=0, Audio\SaveAudio=0) was not honoured`)
	}
}

// A user hive may remove entries from a machine list, never add to it.
func TestUserHiveListOnlyNarrowsTheMachineList(t *testing.T) {
	machine := PolicyValues{ProvidersAllowed: []string{"local", "ollama", "foundry"}}
	user := PolicyValues{ProvidersAllowed: []string{"OLLAMA", "deepgram"}}
	cfg := defaults()

	AttachPolicy(cfg, mergePolicyHives(PolicyValues{}, machine, user))

	if !sameSet(cfg.Providers.Allowed, []string{"ollama"}) {
		t.Fatalf("allowed providers = %v, want only the entry both lists name", cfg.Providers.Allowed)
	}
}

// A policy change while running applies tightenings at once and leaves every
// loosening for the next start.
func TestPolicyRefreshTightensAtOnceAndDefersLoosening(t *testing.T) {
	cfg := defaults()
	cfg.Store.SaveAudio = true
	cfg.Privacy.NetworkScope = NetworkScopeOpen
	userManifest := cfg.Update.ManifestURL

	first := RefreshPolicy(cfg, PolicyValues{
		AudioSaveAudio:      ptrBool(false),
		ProvidersAllowed:    []string{"local", "foundry"},
		PrivacyNetworkScope: NetworkScopeDeviceOnly,
		UpdateManifestURL:   "https://mirror.example.test/latest", // changes where SpeechKit connects
	})
	if cfg.Store.SaveAudio || string(cfg.NetworkScope()) != NetworkScopeDeviceOnly || !sameSet(cfg.Providers.Allowed, []string{"local", "foundry"}) {
		t.Fatalf("tightening not applied: save_audio=%v scope=%q allowed=%v", cfg.Store.SaveAudio, cfg.NetworkScope(), cfg.Providers.Allowed)
	}
	if cfg.Update.ManifestURL != userManifest || !slices.Contains(first.Deferred, `Update\ManifestURL`) {
		t.Fatalf("a new endpoint was applied mid-process: %q (deferred %v)", cfg.Update.ManifestURL, first.Deferred)
	}
	edit := *cfg
	edit.Store.SaveAudio = true
	if err := cfg.CheckPolicyEdit(&edit); err == nil {
		t.Fatal("a value tightened at runtime is not locked")
	}

	second := RefreshPolicy(cfg, PolicyValues{
		// Audio\SaveAudio removed, the scope widened, a provider added and one removed.
		ProvidersAllowed:    []string{"local", "ollama"},
		PrivacyNetworkScope: NetworkScopeOpen,
	})
	if cfg.Store.SaveAudio || cfg.Policy().AudioSaveAudio == nil {
		t.Fatal("removing a pin loosened it before the next start")
	}
	if string(cfg.NetworkScope()) != NetworkScopeDeviceOnly {
		t.Fatalf("a wider scope applied mid-process: %q", cfg.NetworkScope())
	}
	if !sameSet(cfg.Providers.Allowed, []string{"local"}) {
		t.Fatalf("allowed providers = %v, want the removal applied and the addition deferred", cfg.Providers.Allowed)
	}
	if !slices.Contains(second.Deferred, `Audio\SaveAudio`) || !slices.Contains(second.Tightened, `Providers\Allowed`) {
		t.Fatalf("refresh report: tightened %v deferred %v", second.Tightened, second.Deferred)
	}
}

// On macOS only the MDM-delivered file under the managed-preferences
// directory is a policy source; a plist in the user's own library is not.
func TestManagedPreferencesTrustOnlyTheManagedDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPrefs := filepath.Join(home, "Library", "Preferences")
	writePlist(t, filepath.Join(userPrefs, testBundleID+".plist"), `
	<key>Audit.Enabled</key><false/>
	<key>Update.Enabled</key><true/>`)

	managed := t.TempDir()
	if got := readManagedPreferences(managed, testBundleID, nil); got.hasValues() {
		t.Fatalf("a user-library plist became policy: %+v", got)
	}

	writePlist(t, filepath.Join(managed, testBundleID+".plist"), `
	<key>Providers.Allowed</key><array><string>local</string><string>Foundry</string></array>
	<key>Audit.Enabled</key><true/>
	<key>Foundry.EntraClientId</key><string>11111111-1111-1111-1111-111111111111</string>
	<key>Privacy.NetworkScope</key><string>local_network</string>`)
	cfg := defaults()
	AttachPolicy(cfg, readManagedPreferences(managed, testBundleID, nil))

	if !cfg.Audit.Enabled || cfg.Providers.Foundry.EntraClientID != "11111111-1111-1111-1111-111111111111" ||
		!sameSet(cfg.Providers.Allowed, []string{"local", "foundry"}) || string(cfg.NetworkScope()) != NetworkScopeLocalNetwork {
		t.Fatalf("managed preferences not applied: audit=%v client=%q allowed=%v scope=%q",
			cfg.Audit.Enabled, cfg.Providers.Foundry.EntraClientID, cfg.Providers.Allowed, cfg.NetworkScope())
	}
	if !slices.Contains(cfg.PolicyLockedFields(), "providers.allowed") {
		t.Fatalf("managed value not reported locked: %v", cfg.PolicyLockedFields())
	}
}

const testBundleID = "com.example.product"

// A registered identity reads the policy of its own product and never another
// product's; a process that registers none reads no policy at all.
func TestPolicyIdentitySelectsItsOwnPolicySource(t *testing.T) {
	managed := t.TempDir()
	writePlist(t, filepath.Join(managed, "com.kombify.speechkit.plist"), `<key>Audit.Enabled</key><true/>`)
	writePlist(t, filepath.Join(managed, "com.contoso.notes.plist"), `<key>Update.Enabled</key><false/>`)

	productid.ClearForTest(t)
	if got := readManagedPreferencesFor(managed, nil); got.hasValues() || got.Origin != PolicyOriginNoIdentity {
		t.Fatalf("a process without an identity read a policy: %+v", got)
	}

	contoso := productid.ForTests
	contoso.Vendor, contoso.Product, contoso.BundleID = "Contoso", "Notes", "com.contoso.notes"
	productid.UseForTest(t, contoso)
	got := readManagedPreferencesFor(managed, nil)
	if got.UpdateEnabled == nil || *got.UpdateEnabled || got.AuditEnabled != nil {
		t.Fatalf("identity read the wrong product's policy: update=%v audit=%v", got.UpdateEnabled, got.AuditEnabled)
	}
}

// A root that registered no product identity cannot load a configuration: an
// unnamed product has no policy source, and treating that as "no policy"
// would be fail-open on a managed host.
func TestLoadWithoutProductIdentityFailsClosed(t *testing.T) {
	productid.ClearForTest(t)
	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if !errors.Is(err, ErrNoIdentity) || cfg != nil {
		t.Fatalf("Load without identity = %v, %v; want ErrNoIdentity and no config", cfg, err)
	}
}

func writePlist(t *testing.T, path, entries string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>` + entries + `
</dict>
</plist>
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

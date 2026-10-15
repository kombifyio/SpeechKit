package config

// Machine policy (ADMX/GPO on Windows, managed preferences on macOS).
//
// Adding a policy key is a change in this file only:
//
//  1. Add a field to PolicyValues: *bool or *int for DWORD/boolean values,
//     string for REG_SZ values ("" means not set), []string for list values
//     (nil means not set; a present but empty list is non-nil and empty).
//  2. Add one entry to policyKeys with the matching constructor (boolKey,
//     intKey, stringKey or listKey). The entry names the registry value
//     (`Section\Value`; the macOS key is `Section.Value`), the config key it
//     locks, the config field it pins and its trust rule: whether the user hive
//     (HKCU) may set the restrictive value of a boolean, and for strings an
//     optional strictness order and validator.
//
// Both platform readers, the HKCU merge, the lock list for the settings UI,
// CheckPolicyEdit, Save and the runtime refresh pick the key up from that
// entry. Then add the key's texts to app/tools/admxgen/policydocs.go,
// regenerate installer/admx (command in installer/admx/README.md; the quick
// quality gate fails on drift) and add the row in
// docs/runbooks/admx-deployment.md.

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/runtimepath"
	"github.com/kombifyio/SpeechKit/app/internal/safepath"
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Policy sources, in precedence order. A value from an earlier source is
// never replaced by a later one.
const (
	PolicySourceMachinePolicies = "hklm-policies"
	PolicySourceMachineDefaults = "hklm-defaults"
	// PolicySourceManagedPreferences is the macOS MDM-delivered
	// /Library/Managed Preferences/<domain>.plist.
	PolicySourceManagedPreferences = "managed-preferences"
	PolicySourceUser               = "hkcu"
)

// PolicyValues holds the machine-policy subset of Config. Pointer fields
// signal "set vs not set" for booleans and ints; empty strings signal "not
// set" for strings; nil slices signal "not set" for lists.
//
// The struct, the merge and the enforcement logic live here (build-tag
// neutral) so they are tested on every target. Only the source walk is
// platform-specific: policy_windows.go (registry), policy_darwin.go (managed
// preferences) and policy_other.go (empty overlay).
//
// Registry layout consumed on Windows (highest priority first), derived from
// the PolicyIdentity the composition root registers (policy_identity.go):
//
//	HKLM\SOFTWARE\Policies\<Vendor>\<Product>\  — machine policy (GPO)
//	HKLM\SOFTWARE\<Vendor>\<Product>\           — machine-wide admin values
//	HKCU\Software\<Vendor>\<Product>\           — per-user values, restrict-only
//
// Both HKLM hives are writable only by administrators. HKCU is writable by the
// user (and by anything that runs as the user), so it is trusted no more than
// config.toml: mergePolicyHives accepts from it only the restrictive value of
// booleans that allow it, lets a list only remove entries from the machine
// list (set intersection), and drops everything else.
type PolicyValues struct {
	UpdateEnabled             *bool
	UpdateManifestURL         string
	UpdateMode                string
	TelemetryUpdateCheck      *bool
	ProvidersEnforceLocalOnly *bool
	// ProvidersAllowed is the provider allowlist (provider ids such as
	// local, ollama, foundry, github_copilot). It lands in [providers]
	// allowed and is enforced at provider construction (provider_policy.go).
	ProvidersAllowed     []string
	VoiceAgentAllowCloud *bool
	AuditEnabled         *bool
	AuditRetentionDays   *int
	AuditEventLogEnabled *bool
	AuditOTLPEndpoint    string
	// AuditForwardRequired pins [audit] forward_required (a user can only
	// switch it on, never off).
	AuditForwardRequired *bool
	// PrivacyRetentionTTLDays pins [privacy] retention_ttl_days, the hard
	// upper bound in days on stored content (0 pins "no TTL"). The user hive
	// may only shorten it.
	PrivacyRetentionTTLDays *int
	// StorageDataRoot pins [store] data_root: an absolute path on a local
	// volume (see validateDataRoot).
	StorageDataRoot string
	// FoundryEntraClientID and FoundryEntraTenantID let a company pin the
	// Microsoft Entra app registration SpeechKit signs in with (see
	// docs/entra-app-registration.md) for a whole fleet.
	FoundryEntraClientID        string
	FoundryEntraTenantID        string
	FoundryProjectEndpoint      string
	FoundryAuthMode             string
	FoundryEntraAllowCustomHost *bool
	OllamaBaseURL               string
	CopilotEnabled              *bool
	CopilotCLIPath              string
	PrivacyNetworkScope         string
	PrivacyRetentionScope       string
	AudioSaveAudio              *bool
	AssistCaptureSelection      *bool
	MeetingScreenshotsEnabled   *bool
	MeetingScreenshotHotkey     *bool
	Microsoft365TenantID        string
	Microsoft365ImportTeams     *bool
	Microsoft365ImportRecaps    *bool
	// Microsoft365AllowTranscriptPaste pins [microsoft365]
	// allow_transcript_paste; HKCU may set it off.
	Microsoft365AllowTranscriptPaste *bool
	// MeetingCaptureChannels pins [meeting] capture_channels (mic, system).
	MeetingCaptureChannels []string
	// ModelsSource pins [models] source: "public", "bundled" or the https base
	// URL of an internal model mirror (see models_source.go).
	ModelsSource string
	// MeetingScreenshotDeniedProcesses pins [meeting] screenshot_denied_processes:
	// process image names whose windows are never captured. The user hive
	// (HKCU) may only add entries.
	MeetingScreenshotDeniedProcesses []string
	// OneNoteEnabled pins [onenote] enabled; OneNoteAllowedNotebookScope
	// pins [onenote] allowed_notebook_scope ("personal" or a site id).
	OneNoteEnabled              *bool
	OneNoteAllowedNotebookScope string

	// Origin describes which source first contributed a value. One of
	// "hklm-policies" | "hklm-defaults" | "managed-preferences" | "hkcu" |
	// "none" | "non-windows".
	Origin string
	// KeysFound counts the policy values in this overlay. Every one of them
	// is enforced for the life of the process. Used in the policy.applied
	// audit event.
	KeysFound int
	// MachineKeys and UserKeys split KeysFound by trust level: values from an
	// administrator source vs. restrict-only values from HKCU.
	MachineKeys int
	UserKeys    int
	// IgnoredUserKeys counts HKCU values that were dropped because they would
	// have loosened a setting (or are security-relevant and machine-only).
	IgnoredUserKeys int
	// Rejected lists values that were present but not applied (wrong type,
	// failed validation). A rejected scope value fails closed to its strictest
	// setting instead of being dropped.
	Rejected []PolicyRejection

	// origins maps a policy key name to the source of its value.
	origins map[string]string
}

// PolicyRejection records one policy value that was read but not applied.
// Err wraps ErrPolicyValueInvalid; it names the problem, never the value of
// a secret.
type PolicyRejection struct {
	Key string
	Err error
}

var (
	// ErrPolicyLocked is returned when an edit tries to change a value an
	// administrator policy pins.
	ErrPolicyLocked = errors.New("this setting is managed by an administrator policy and cannot be changed here")
	// ErrPolicyValueInvalid wraps every rejected policy value.
	ErrPolicyValueInvalid = errors.New("policy value rejected")
)

// EnforcesLocalOnly reports whether Providers\EnforceLocalOnly=1 is in force.
func (p PolicyValues) EnforcesLocalOnly() bool {
	return p.ProvidersEnforceLocalOnly != nil && *p.ProvidersEnforceLocalOnly
}

// BlocksCloudVoiceAgent reports whether VoiceAgent\AllowCloudProviders=0 is
// in force.
func (p PolicyValues) BlocksCloudVoiceAgent() bool {
	return p.VoiceAgentAllowCloud != nil && !*p.VoiceAgentAllowCloud
}

// rejectedKey reports whether the policy read rejected the value of key.
func (p PolicyValues) rejectedKey(key string) bool {
	for _, r := range p.Rejected {
		if r.Key == key {
			return true
		}
	}
	return false
}

func (p PolicyValues) hasValues() bool {
	for _, f := range policyFields {
		if f.pinned(p) {
			return true
		}
	}
	return false
}

// policyKeys is the single table of policy keys. See the comment at the top
// of this file for how to add one.
var policyKeys = []policyKey{
	boolKey(`Update\Enabled`, "update.enabled",
		func(p *PolicyValues) **bool { return &p.UpdateEnabled },
		func(c *Config) *bool { return &c.Update.Enabled },
		false, userMayRestrict),
	stringKey(`Update\ManifestURL`, "update.manifest_url",
		func(p *PolicyValues) *string { return &p.UpdateManifestURL },
		func(c *Config) *string { return &c.Update.ManifestURL },
		stringRules{}),
	// notify is stricter than install: it applies at once, a switch back waits for
	// the next start. The user hive may only set notify. An unknown value fails
	// closed to notify, so a typo never turns installs on.
	stringKey(`Update\Mode`, "update.mode",
		func(p *PolicyValues) *string { return &p.UpdateMode },
		func(c *Config) *string { return &c.Update.Mode },
		stringRules{
			order:    []string{UpdateModeInstall, UpdateModeNotify},
			validate: validateUpdateMode,
			invalid:  UpdateModeNotify,
			userMay:  UpdateModeNotify,
		}),
	boolKey(`Telemetry\UpdateCheck`, "telemetry.update_check",
		func(p *PolicyValues) **bool { return &p.TelemetryUpdateCheck },
		func(c *Config) *bool { return &c.Telemetry.UpdateCheck },
		false, userMayRestrict),
	// EnforceLocalOnly=1 pins the STT routing strategy to the on-device
	// model. NetworkScope additionally caps the scope for every other
	// surface (see privacy.go).
	derivedBoolKey(`Providers\EnforceLocalOnly`,
		func(p *PolicyValues) **bool { return &p.ProvidersEnforceLocalOnly },
		true, policyField{
			name:   "routing.strategy",
			pinned: PolicyValues.EnforcesLocalOnly,
			apply:  func(c *Config, _ PolicyValues) { c.Routing.Strategy = "local-only" },
			equal:  func(a, b *Config) bool { return a.Routing.Strategy == b.Routing.Strategy },
			copy:   func(d, s *Config) { d.Routing.Strategy = s.Routing.Strategy },
		}),
	listKey(`Providers\Allowed`, "providers.allowed",
		func(p *PolicyValues) *[]string { return &p.ProvidersAllowed },
		func(c *Config) *[]string { return &c.Providers.Allowed }),
	// AllowCloudProviders=0 pins Voice Agent to the cascaded pipeline.
	derivedBoolKey(`VoiceAgent\AllowCloudProviders`,
		func(p *PolicyValues) **bool { return &p.VoiceAgentAllowCloud },
		false, policyField{
			name:   "voice_agent.provider",
			pinned: PolicyValues.BlocksCloudVoiceAgent,
			apply:  func(c *Config, _ PolicyValues) { c.VoiceAgent.Provider = "local-cascaded" },
			equal:  func(a, b *Config) bool { return a.VoiceAgent.Provider == b.VoiceAgent.Provider },
			copy:   func(d, s *Config) { d.VoiceAgent.Provider = s.VoiceAgent.Provider },
		}),
	boolKey(`Audit\Enabled`, "audit.enabled",
		func(p *PolicyValues) **bool { return &p.AuditEnabled },
		func(c *Config) *bool { return &c.Audit.Enabled },
		true, machineOnly),
	intKey(`Audit\RetentionDays`, "audit.retention_days",
		func(p *PolicyValues) **int { return &p.AuditRetentionDays },
		func(c *Config) *int { return &c.Audit.RetentionDays }),
	boolKey(`Audit\EventLogEnabled`, "audit.event_log_enabled",
		func(p *PolicyValues) **bool { return &p.AuditEventLogEnabled },
		func(c *Config) *bool { return &c.Audit.EventLogEnabled },
		true, userMayRestrict),
	// A user may switch forwarding on, never off.
	boolKey(`Audit\ForwardRequired`, "audit.forward_required",
		func(p *PolicyValues) **bool { return &p.AuditForwardRequired },
		func(c *Config) *bool { return &c.Audit.ForwardRequired },
		true, userMayRestrict),
	stringKey(`Audit\OTLPEndpoint`, "audit.otlp_endpoint",
		func(p *PolicyValues) *string { return &p.AuditOTLPEndpoint },
		func(c *Config) *string { return &c.Audit.OTLPEndpoint },
		stringRules{}),
	stringKey(`Foundry\EntraClientId`, "providers.foundry.entra_client_id",
		func(p *PolicyValues) *string { return &p.FoundryEntraClientID },
		func(c *Config) *string { return &c.Providers.Foundry.EntraClientID },
		stringRules{}),
	stringKey(`Foundry\EntraTenantId`, "providers.foundry.entra_tenant_id",
		func(p *PolicyValues) *string { return &p.FoundryEntraTenantID },
		func(c *Config) *string { return &c.Providers.Foundry.EntraTenantID },
		stringRules{}),
	stringKey(`Foundry\ProjectEndpoint`, "providers.foundry.project_endpoint",
		func(p *PolicyValues) *string { return &p.FoundryProjectEndpoint },
		func(c *Config) *string { return &c.Providers.Foundry.ProjectEndpoint },
		stringRules{validate: validatePolicyHTTPURL("https")}),
	stringKey(`Foundry\AuthMode`, "providers.foundry.auth_mode",
		func(p *PolicyValues) *string { return &p.FoundryAuthMode },
		func(c *Config) *string { return &c.Providers.Foundry.AuthMode },
		stringRules{order: []string{FoundryAuthModeAPIKey, FoundryAuthModeEntra}}),
	boolKey(`Foundry\EntraAllowCustomHost`, "providers.foundry.entra_allow_custom_host",
		func(p *PolicyValues) **bool { return &p.FoundryEntraAllowCustomHost },
		func(c *Config) *bool { return &c.Providers.Foundry.EntraAllowCustomHost },
		false, machineOnly),
	stringKey(`Ollama\BaseURL`, "providers.ollama.base_url",
		func(p *PolicyValues) *string { return &p.OllamaBaseURL },
		func(c *Config) *string { return &c.Providers.Ollama.BaseURL },
		stringRules{validate: validateOllamaBaseURL}),
	boolKey(`Copilot\Enabled`, "copilot.enabled",
		func(p *PolicyValues) **bool { return &p.CopilotEnabled },
		func(c *Config) *bool { return &c.Copilot.Enabled },
		false, machineOnly),
	stringKey(`Copilot\CliPath`, "copilot.cli_path",
		func(p *PolicyValues) *string { return &p.CopilotCLIPath },
		func(c *Config) *string { return &c.Copilot.CLIPath },
		stringRules{validate: validateCopilotCLIPath}),
	// The stricter of this pin and the EnforceLocalOnly cap wins: see
	// Config.NetworkScope. An invalid value fails closed to device_only.
	stringKey(`Privacy\NetworkScope`, "privacy.network_scope",
		func(p *PolicyValues) *string { return &p.PrivacyNetworkScope },
		func(c *Config) *string { return &c.Privacy.NetworkScope },
		stringRules{
			order:    []string{NetworkScopeOpen, NetworkScopeLocalNetwork, NetworkScopeDeviceOnly},
			validate: func(v string) (string, error) { s, err := framework.ParseNetworkScope(v); return string(s), err },
			invalid:  NetworkScopeDeviceOnly,
		}),
	stringKey(`Privacy\RetentionScope`, "privacy.retention_scope",
		func(p *PolicyValues) *string { return &p.PrivacyRetentionScope },
		func(c *Config) *string { return &c.Privacy.RetentionScope },
		stringRules{
			order:    []string{RetentionScopeRetain, RetentionScopeEphemeral},
			validate: func(v string) (string, error) { s, err := framework.ParseRetentionScope(v); return string(s), err },
			invalid:  RetentionScopeEphemeral,
		}),
	// The user hive may only shorten the TTL: a smaller non-zero number of
	// days than the machine value (any non-zero value when none is pinned).
	intKey(`Privacy\RetentionTTLDays`, "privacy.retention_ttl_days",
		func(p *PolicyValues) **int { return &p.PrivacyRetentionTTLDays },
		func(c *Config) *int { return &c.Privacy.RetentionTTLDays }).
		withIntRules(nonNegativeDays, shorterTTL),
	// Applied by Load, so it is bound before the desktop resolves its data
	// root; an invalid value is rejected and the configured root stays.
	stringKey(`Storage\DataRoot`, "store.data_root",
		func(p *PolicyValues) *string { return &p.StorageDataRoot },
		func(c *Config) *string { return &c.Store.DataRoot },
		stringRules{validate: validateDataRoot}),
	boolKey(`Audio\SaveAudio`, "store.save_audio",
		func(p *PolicyValues) **bool { return &p.AudioSaveAudio },
		func(c *Config) *bool { return &c.Store.SaveAudio },
		false, userMayRestrict),
	boolKey(`Assist\CaptureSelection`, "assist.capture_selection",
		func(p *PolicyValues) **bool { return &p.AssistCaptureSelection },
		func(c *Config) *bool { return &c.Assist.CaptureSelection },
		false, userMayRestrict),
	boolKey(`Meeting\ScreenshotsEnabled`, "meeting.screenshot_enabled",
		func(p *PolicyValues) **bool { return &p.MeetingScreenshotsEnabled },
		func(c *Config) *bool { return &c.Meeting.ScreenshotEnabled },
		false, userMayRestrict),
	boolKey(`Meeting\ScreenshotHotkeyEnabled`, "meeting.screenshot_hotkey_enabled",
		func(p *PolicyValues) **bool { return &p.MeetingScreenshotHotkey },
		func(c *Config) *bool { return &c.Meeting.ScreenshotHotkeyEnabled },
		false, userMayRestrict),
	// HKCU may only remove channels. An unreadable, empty or emptied pin
	// applies as microphone only, never as the two-channel default.
	meetingCaptureChannelsKey(),
	// A deny list only ever narrows what is captured, so the user hive may add
	// entries and never remove one.
	denyListKey(`Meeting\ScreenshotDeniedProcesses`, "meeting.screenshot_denied_processes",
		func(p *PolicyValues) *[]string { return &p.MeetingScreenshotDeniedProcesses },
		func(c *Config) *[]string { return &c.Meeting.ScreenshotDeniedProcesses }),
	stringKey(`Microsoft365\TenantId`, "microsoft365.tenant_id",
		func(p *PolicyValues) *string { return &p.Microsoft365TenantID },
		func(c *Config) *string { return &c.Microsoft365.TenantID },
		stringRules{}),
	boolKey(`Microsoft365\ImportTeamsTranscripts`, "microsoft365.import_teams_transcripts",
		func(p *PolicyValues) **bool { return &p.Microsoft365ImportTeams },
		func(c *Config) *bool { return &c.Microsoft365.ImportTeamsTranscripts },
		false, userMayRestrict),
	boolKey(`Microsoft365\ImportCopilotRecaps`, "microsoft365.import_copilot_recaps",
		func(p *PolicyValues) **bool { return &p.Microsoft365ImportRecaps },
		func(c *Config) *bool { return &c.Microsoft365.ImportCopilotRecaps },
		false, userMayRestrict),
	// Pasting or uploading a transcript into a finished meeting; it also
	// needs the Teams import and its permission. HKCU may only switch it off.
	boolKey(`Microsoft365\AllowTranscriptPaste`, "microsoft365.allow_transcript_paste",
		func(p *PolicyValues) **bool { return &p.Microsoft365AllowTranscriptPaste },
		func(c *Config) *bool { return &c.Microsoft365.AllowTranscriptPaste },
		false, userMayRestrict),
	// public, bundled or an https mirror base URL. A mirror closes the public
	// hosts, bundled closes every download; both apply at once, a step back
	// waits for the next start. The user hive may only set bundled, and only
	// when no machine value exists. An unusable value fails closed to bundled.
	stringKey(`Models\Source`, "models.source",
		func(p *PolicyValues) *string { return &p.ModelsSource },
		func(c *Config) *string { return &c.Models.Source },
		stringRules{
			strictness: modelsSourceStrictness,
			validate:   validateModelsSource,
			invalid:    ModelsSourceBundled,
			userMay:    ModelsSourceBundled,
		}),
	boolKey(`OneNote\Enabled`, "onenote.enabled",
		func(p *PolicyValues) **bool { return &p.OneNoteEnabled },
		func(c *Config) *bool { return &c.OneNote.Enabled },
		false, userMayRestrict),
	// An invalid scope fails closed to the user's own notebooks; the user
	// hive may only ask for those, and only without a machine value.
	stringKey(`OneNote\AllowedNotebookScope`, "onenote.allowed_notebook_scope",
		func(p *PolicyValues) *string { return &p.OneNoteAllowedNotebookScope },
		func(c *Config) *string { return &c.OneNote.AllowedNotebookScope },
		stringRules{validate: validateOneNoteScope, invalid: OneNoteScopePersonal, userMay: OneNoteScopePersonal}),
}

// policyFields are the config pins the keys produce, in table order.
var policyFields = buildPolicyFields(policyKeys)

// copilotExecutableNames are the only file names a pinned Copilot CLI path may
// name.
var copilotExecutableNames = []string{"copilot", "copilot.exe"}

// copilotInstallRoots are the directories a pinned Copilot CLI must live
// under: the install root, the directory of the running executable (an
// administrator-owned location for an installed product). A seam for tests.
var copilotInstallRoots = func() []string {
	if dir := runtimepath.ExecutableDir(); dir != "" {
		return []string{dir}
	}
	return nil
}

// validateCopilotCLIPath applies the safepath rules azure_cli_path uses (an
// absolute path on a local volume, no UNC path or mapped network drive,
// naming an existing regular file called copilot or copilot.exe) and requires
// the file to lie under the install root, so a policy cannot point the
// generator at a binary in a user-writable location.
func validateCopilotCLIPath(v string) (string, error) {
	if err := safepath.Executable(v, copilotExecutableNames...); err != nil {
		return "", err
	}
	roots := copilotInstallRoots()
	if len(roots) == 0 {
		return "", errors.New("the install root is unknown")
	}
	if _, err := safepath.Contained(v, roots...); err != nil {
		return "", fmt.Errorf("not under the install root: %w", err)
	}
	return v, nil
}

// validateDataRoot applies the safepath rule the data root uses at start-up:
// absolute, on a local volume, no UNC path or mapped network drive.
func validateDataRoot(v string) (string, error) {
	if err := safepath.LocalAbsolute(v); err != nil {
		return "", err
	}
	return v, nil
}

// Update modes ([update] mode, Update\Mode), least to most strict.
const (
	UpdateModeInstall = "install"
	UpdateModeNotify  = "notify"
)

// validateUpdateMode normalizes an Update\Mode value or rejects it.
func validateUpdateMode(v string) (string, error) {
	switch s := strings.ToLower(strings.TrimSpace(v)); s {
	case UpdateModeInstall, UpdateModeNotify:
		return s, nil
	}
	return "", fmt.Errorf("unknown update mode %q (want %q or %q)", v, UpdateModeInstall, UpdateModeNotify)
}

// nonNegativeDays rejects a negative day count.
func nonNegativeDays(v int) error {
	if v < 0 {
		return errors.New("must not be negative")
	}
	return nil
}

// shorterTTL reports whether next is a stricter retention TTL than cur,
// where 0 means no TTL (the longest).
func shorterTTL(cur, next int) bool { return next > 0 && (cur == 0 || next < cur) }

// validateOllamaBaseURL accepts an http(s) URL whose host is a loopback
// address (localhost, 127.0.0.0/8, ::1). A pinned Ollama endpoint anywhere
// else is rejected, and a rejected pin leaves Ollama unbuilt
// (Config.ProviderAllowed).
func validateOllamaBaseURL(v string) (string, error) {
	if _, err := validatePolicyHTTPURL("http", "https")(v); err != nil {
		return "", err
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return v, nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return v, nil
	}
	return "", errors.New("the Ollama endpoint must be a loopback address (127.0.0.1, ::1 or localhost)")
}

// validatePolicyHTTPURL accepts an absolute URL with one of schemes and a
// host. Where a pinned endpoint may be dialled is decided at provider
// construction; this only refuses values that are not endpoints at all.
func validatePolicyHTTPURL(schemes ...string) func(string) (string, error) {
	return func(v string) (string, error) {
		u, err := url.Parse(v)
		if err != nil {
			return "", err
		}
		if u.Host == "" {
			return "", errors.New("not an absolute URL")
		}
		for _, s := range schemes {
			if strings.EqualFold(u.Scheme, s) {
				return v, nil
			}
		}
		return "", fmt.Errorf("scheme must be %s", strings.Join(schemes, " or "))
	}
}

// readPolicyHive reads every policy key from one source. A value that is
// present but unusable is recorded in Rejected; it never fails the read.
func readPolicyHive(src policySource) PolicyValues {
	var p PolicyValues
	for _, k := range policyKeys {
		if err := k.ops.load(src, k.name, &p); err != nil {
			p.Rejected = append(p.Rejected, PolicyRejection{Key: k.name, Err: fmt.Errorf("%w: %w", ErrPolicyValueInvalid, err)})
		}
	}
	return p
}

// mergePolicyHives resolves the effective overlay from the three Windows
// registry hives. Precedence is per key: HKLM\SOFTWARE\Policies\<Vendor>\<Product> beats HKLM\SOFTWARE\<Vendor>\<Product>
// beats HKCU. HKCU contributes only restrictive booleans that allow it and
// narrows machine lists; everything else from HKCU is counted as ignored.
func mergePolicyHives(machinePolicies, machineDefaults, user PolicyValues) PolicyValues {
	result := PolicyValues{origins: map[string]string{}}
	machine := []struct {
		values PolicyValues
		source string
	}{
		{machinePolicies, PolicySourceMachinePolicies},
		{machineDefaults, PolicySourceMachineDefaults},
	}
	for _, k := range policyKeys {
		for _, h := range machine {
			if !k.ops.set(result) && k.ops.set(h.values) {
				k.ops.take(&result, h.values)
				result.origins[k.name] = h.source
			}
		}
		switch k.ops.userMerge(&result, user) {
		case userContributed:
			result.origins[k.name] = PolicySourceUser
		case userIgnored:
			result.IgnoredUserKeys++
		case userNone:
		}
	}
	for _, h := range [][]PolicyRejection{machinePolicies.Rejected, machineDefaults.Rejected, user.Rejected} {
		result.Rejected = append(result.Rejected, h...)
	}
	result.finalize()
	return result
}

// singleSourcePolicy turns the values read from one trusted source (macOS
// managed preferences) into an overlay.
func singleSourcePolicy(values PolicyValues, source string) PolicyValues {
	values.origins = map[string]string{}
	for _, k := range policyKeys {
		if k.ops.set(values) {
			values.origins[k.name] = source
		}
	}
	values.finalize()
	return values
}

// finalize derives Origin and the key counts from origins.
func (p *PolicyValues) finalize() {
	p.KeysFound, p.MachineKeys, p.UserKeys = 0, 0, 0
	for _, source := range p.origins {
		p.KeysFound++
		if source == PolicySourceUser {
			p.UserKeys++
		} else {
			p.MachineKeys++
		}
	}
	p.Origin = "none"
	for _, source := range []string{PolicySourceMachinePolicies, PolicySourceMachineDefaults, PolicySourceManagedPreferences, PolicySourceUser} {
		if p.hasOrigin(source) {
			p.Origin = source
			return
		}
	}
}

func (p PolicyValues) hasOrigin(source string) bool {
	for _, s := range p.origins {
		if s == source {
			return true
		}
	}
	return false
}

// applyPolicyOverlay mutates cfg in place with the values from policy.
// Policy values always win over config.toml values.
func applyPolicyOverlay(cfg *Config, policy PolicyValues) {
	for _, f := range policyFields {
		if f.pinned(policy) {
			f.apply(cfg, policy)
		}
	}
}

// policyField describes one config value a policy can pin.
type policyField struct {
	name   string
	pinned func(p PolicyValues) bool
	apply  func(cfg *Config, p PolicyValues)
	equal  func(a, b *Config) bool
	copy   func(dst, src *Config)
}

// policyBinding is the policy a Config was loaded under. It travels with
// every copy of the Config (nextCfg := *cfg) so enforcement points and Save
// see it without a global.
type policyBinding struct {
	values PolicyValues
	// user holds the config values as they were before the overlay, so Save
	// writes the user's own choices for pinned keys and removing the policy
	// restores them.
	user *Config
}

// AttachPolicy binds policy to cfg and applies it. The values stay enforced
// for the life of the process: EnforcePolicy re-applies them, NetworkScope
// honours EnforceLocalOnly, and Save never persists them over the user's own
// values. Load calls it; tests and embedders that build a Config by hand use
// it to simulate a machine policy.
func AttachPolicy(cfg *Config, policy PolicyValues) {
	if cfg == nil {
		return
	}
	if cfg.policy != nil {
		// Re-attaching starts from the user's own values, not from the
		// previous overlay.
		for _, f := range policyFields {
			if f.pinned(cfg.policy.values) {
				f.copy(cfg, cfg.policy.user)
			}
		}
		cfg.policy = nil
	}
	// A read with only rejected values still binds: enforcement points ask
	// for the rejections (a rejected Ollama pin leaves Ollama unbuilt).
	if !policy.hasValues() && len(policy.Rejected) == 0 {
		return
	}
	user := *cfg
	cfg.policy = &policyBinding{values: policy, user: &user}
	applyPolicyOverlay(cfg, policy)
}

// Policy returns the policy cfg was loaded under (zero value when none).
func (c *Config) Policy() PolicyValues {
	if c == nil || c.policy == nil {
		return PolicyValues{}
	}
	return c.policy.values
}

// EnforcePolicy re-applies the bound policy to cfg. Call it after code that
// derives pinned values from other settings (routing reconciliation, profile
// selection, presets) so a derived value can never undo the policy.
func EnforcePolicy(cfg *Config) {
	if cfg == nil || cfg.policy == nil {
		return
	}
	applyPolicyOverlay(cfg, cfg.policy.values)
}

// PolicyLockedFields names the config keys the bound policy pins, for the
// settings UI. "privacy.network_scope" is listed when EnforceLocalOnly caps
// the effective network scope.
func (c *Config) PolicyLockedFields() []string {
	if c == nil || c.policy == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, f := range policyFields {
		if f.pinned(c.policy.values) {
			add(f.name)
		}
	}
	if c.policy.values.EnforcesLocalOnly() {
		add("privacy.network_scope")
	}
	return names
}

// PolicyLockedError is the error CheckPolicyEdit returns for an edit a policy
// (or a managed host's untrusted config) refuses. It matches ErrPolicyLocked
// under errors.Is and carries the config keys the edit touched, so callers
// branch and audit on the keys instead of parsing the message.
type PolicyLockedError struct {
	// Keys are the config keys (dotted TOML paths) the edit tried to change.
	Keys []string
}

func (e *PolicyLockedError) Error() string {
	return ErrPolicyLocked.Error() + " (" + strings.Join(e.Keys, ", ") + ")"
}

func (e *PolicyLockedError) Unwrap() error { return ErrPolicyLocked }

// PolicyLockedKeys returns the config keys carried by an error wrapping a
// PolicyLockedError, nil for any other error.
func PolicyLockedKeys(err error) []string {
	var locked *PolicyLockedError
	if errors.As(err, &locked) {
		return slices.Clone(locked.Keys)
	}
	return nil
}

// PolicyNameForKey returns the machine-policy value (`Section\Value`) that
// pins the config key, "" when no policy key pins it.
func PolicyNameForKey(configKey string) string {
	for _, k := range policyKeys {
		if k.field == configKey {
			return k.name
		}
	}
	return ""
}

// CheckPolicyEdit rejects next when it changes a value the policy bound to c
// pins, and on a managed host when it changes any key outside the
// user-editable allowlist (managed_config.go). Unchanged values pass, so a
// form that echoes the enforced value back is not an error. The error is a
// *PolicyLockedError naming every refused key.
func (c *Config) CheckPolicyEdit(next *Config) error {
	if c == nil || next == nil {
		return nil
	}
	var keys []string
	if ManagedMode() {
		keys = managedEditViolations(c, next)
	}
	if c.policy != nil {
		for _, f := range policyFields {
			if f.pinned(c.policy.values) && !f.equal(c, next) && !slices.Contains(keys, f.name) {
				keys = append(keys, f.name)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return &PolicyLockedError{Keys: keys}
}

// persistable returns the Config Save writes: cfg with every pinned key
// replaced by the user's own pre-policy value.
func persistable(cfg *Config) *Config {
	if cfg == nil || cfg.policy == nil {
		return cfg
	}
	out := *cfg
	for _, f := range policyFields {
		if f.pinned(cfg.policy.values) {
			f.copy(&out, cfg.policy.user)
		}
	}
	return &out
}

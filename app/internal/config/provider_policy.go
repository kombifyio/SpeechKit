package config

// provider_policy.go is where the provider allowlist and the endpoint pins
// the machine policy carries (policy.go) become decisions. Every place that
// constructs a provider asks these methods first, so a provider outside the
// allowlist is never built and a pinned endpoint is the only one dialled.
//
// Two hosts read the allowlist differently:
//
//   - The reference client (ManagedMode off): an empty or absent list allows
//     every provider, which is the behaviour before the allowlist existed.
//   - A managed host (ManagedMode on): an empty or absent list allows the
//     on-device providers only, and without any machine policy at all the
//     same holds with its own reason. Config is untrusted there; see
//     managed_config.go.

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ProviderIDGitHubCopilot is the allowlist id of the GitHub Copilot generator.
const ProviderIDGitHubCopilot = "github_copilot"

// ErrProviderNotAllowed is returned (wrapped) where a provider is refused by
// the allowlist, the managed-mode default or the retention scope. The
// stable reason id travels with it.
var ErrProviderNotAllowed = errors.New("provider not allowed by policy")

var managedMode atomic.Bool

// SetManagedMode marks the process as a managed host. The composition root
// of a managed product sets it once at start-up, before Load; the reference
// client leaves it off. Under managed mode an empty provider allowlist allows
// on-device providers only, a missing machine policy starts no cloud
// provider, and config.toml contributes the user-editable keys only.
func SetManagedMode(on bool) { managedMode.Store(on) }

// ManagedMode reports whether the composition root marked this process as a
// managed host.
func ManagedMode() bool { return managedMode.Load() }

// HasMachinePolicy reports whether an administrator source (HKLM policies,
// HKLM defaults or macOS managed preferences) contributed to the bound
// policy. Per-user registry values alone are not a machine policy.
func (c *Config) HasMachinePolicy() bool {
	p := c.Policy()
	if p.MachineKeys > 0 {
		return true
	}
	switch p.Origin {
	case PolicySourceMachinePolicies, PolicySourceMachineDefaults, PolicySourceManagedPreferences:
		return true
	default:
		return false
	}
}

// onDeviceProvider reports whether provider runs on this machine without a
// network service of its own (the bundled whisper, llama and Piper engines).
func onDeviceProvider(provider string) bool {
	return catalog.ProviderFamily(provider) == "local"
}

// providerAllowlist is the list the decision reads. A managed host takes it
// from the machine policy only; the reference client also honours a list in
// config.toml, which can only narrow what it would otherwise allow.
func (c *Config) providerAllowlist() []string {
	if c == nil {
		return nil
	}
	if ManagedMode() {
		return c.Policy().ProvidersAllowed
	}
	return c.Providers.Allowed
}

// ProviderAllowed reports whether the allowlist admits provider (a provider
// id, alias or adapter name; compared by catalog.ProviderFamily), with the
// stable disabled-reason id when it does not. It does not look at the network
// or retention scopes.
func (c *Config) ProviderAllowed(provider string) (bool, string) {
	ok, reason, _ := c.providerDecision(provider)
	return ok, reason
}

// providerDecision is ProviderAllowed plus the name of the policy that
// refused (empty when allowed).
func (c *Config) providerDecision(provider string) (bool, string, string) {
	if c == nil {
		return true, "", ""
	}
	if strings.TrimSpace(provider) == "" {
		return false, framework.PolicyReasonProviderNotAllowed, policyProvidersAllowed
	}
	managed := ManagedMode()
	if managed && !c.HasMachinePolicy() {
		if onDeviceProvider(provider) {
			return true, "", ""
		}
		return false, framework.PolicyReasonNoMachinePolicy, policyMachinePolicy
	}
	if catalog.ProviderFamily(provider) == "ollama" && c.Policy().rejectedKey(`Ollama\BaseURL`) {
		// A pinned Ollama URL that failed validation (not loopback) leaves
		// Ollama unbuilt rather than falling back to the configured URL.
		return false, framework.PolicyReasonProviderNotAllowed, `Ollama\BaseURL`
	}
	allowed := c.providerAllowlist()
	if len(allowed) == 0 {
		if !managed || onDeviceProvider(provider) {
			return true, "", ""
		}
		return false, framework.PolicyReasonProviderNotAllowed, policyProvidersAllowed
	}
	if catalog.ProviderInAllowlist(provider, allowed) {
		return true, "", ""
	}
	return false, framework.PolicyReasonProviderNotAllowed, policyProvidersAllowed
}

// ProviderConstructionAllowed is the check every provider construction point
// runs: the allowlist (ProviderAllowed) and, for a modality with option
// manifests (provideropts.ModalitySTT, ModalityTTS, ModalityVoiceAgent), the
// retention scope (ProviderAllowedUnderRetention). adapter is the adapter
// identifier the option manifests use ("vps", "gemini", "piper", ...); an
// empty modality skips the retention check (LLM surfaces have no manifest).
func (c *Config) ProviderConstructionAllowed(adapter, modality string) (bool, string) {
	if ok, reason, policy := c.providerDecision(adapter); !ok {
		reportPolicyBlocked(policy, adapter)
		return false, reason
	}
	if c == nil || modality == "" || userOperatedProvider(adapter) {
		return true, ""
	}
	ok, reason := c.ProviderAllowedUnderRetention(adapter, modality)
	if !ok {
		reportPolicyBlocked(`Privacy\RetentionScope`, adapter)
	}
	return ok, reason
}

// userOperatedProvider reports whether provider runs on this device or on a
// host the user operates (local engines, Ollama, a self-hosted server). The
// option manifests declare no-store by default for these on every surface,
// including surfaces that have no manifest of their own.
func userOperatedProvider(provider string) bool {
	switch catalog.ProviderFamily(provider) {
	case "local", "ollama", "selfhosted":
		return true
	default:
		return false
	}
}

// ProfileAllowedUnderRetention is ProviderAllowedUnderRetention for a catalog
// profile: the option manifest that lists profileID decides, so a profile
// served by another adapter of the same provider (Gemini transcription under
// google) is judged by its own surface. Without such a manifest the provider
// id is used.
func (c *Config) ProfileAllowedUnderRetention(profileID, provider, modality string) (bool, string) {
	if userOperatedProvider(provider) {
		return true, ""
	}
	for _, manifest := range provideropts.DefaultManifests() {
		if manifest.Modality != modality {
			continue
		}
		for _, id := range manifest.ProfileIDs {
			if id == profileID {
				return c.ProviderAllowedUnderRetention(manifest.Provider, modality)
			}
		}
	}
	return c.ProviderAllowedUnderRetention(provider, modality)
}

// CopilotAllowed reports whether the GitHub Copilot generator may be built:
// the allowlist names github_copilot (or the reference client has no list)
// and Copilot is enabled, where a Copilot\Enabled=0 pin wins over config.
func (c *Config) CopilotAllowed() (bool, string) {
	if c == nil {
		return false, framework.PolicyReasonProviderNotAllowed
	}
	if ok, reason, policy := c.providerDecision(ProviderIDGitHubCopilot); !ok {
		reportPolicyBlocked(policy, ProviderIDGitHubCopilot)
		return false, reason
	}
	if pinned := c.Policy().CopilotEnabled; pinned != nil && !*pinned {
		reportPolicyBlocked(`Copilot\Enabled`, ProviderIDGitHubCopilot)
		return false, framework.PolicyReasonProviderNotAllowed
	}
	if c.Copilot.Enabled && c.Policy().rejectedKey(`Copilot\CliPath`) {
		// A pinned CLI path that failed validation leaves the generator
		// unbuilt rather than falling back to the configured path.
		reportPolicyBlocked(`Copilot\CliPath`, "copilot.cli_path")
		return false, framework.PolicyReasonProviderNotAllowed
	}
	return c.Copilot.Enabled, ""
}

// FoundryProjectEndpoint is the project endpoint every Foundry adapter
// derives its hosts from. When Foundry\ProjectEndpoint is pinned it is the
// pinned value, whatever config or a settings route put into
// [providers.foundry] project_endpoint; a differing value is ignored and
// reported once.
func (c *Config) FoundryProjectEndpoint() string {
	if c == nil {
		return ""
	}
	configured := strings.TrimSpace(c.Providers.Foundry.ProjectEndpoint)
	pinned := strings.TrimSpace(c.Policy().FoundryProjectEndpoint)
	if pinned == "" {
		return configured
	}
	if configured != "" && configured != pinned {
		reportIgnoredEndpoint(`Foundry\ProjectEndpoint`, "providers.foundry.project_endpoint")
	}
	return pinned
}

// FoundryEntraAllowCustomHost is the effective entra_allow_custom_host: a
// Foundry\EntraAllowCustomHost pin wins over the configured value, so with
// the pin at 0 a token never leaves the Azure AI hosts.
func (c *Config) FoundryEntraAllowCustomHost() bool {
	if c == nil {
		return false
	}
	if pinned := c.Policy().FoundryEntraAllowCustomHost; pinned != nil {
		return *pinned
	}
	return c.Providers.Foundry.EntraAllowCustomHost
}

// FoundryUsesEntra is the effective auth mode: Foundry\AuthMode = entra
// pinned means Entra, whatever the in-memory config says.
func (c *Config) FoundryUsesEntra() bool {
	if c == nil {
		return false
	}
	if c.foundryKeyForbidden() {
		return true
	}
	return c.Providers.Foundry.UsesEntra()
}

// foundryKeyForbidden reports whether Foundry\AuthMode = entra is pinned, in
// which case an API key from config or the secret store is never used.
func (c *Config) foundryKeyForbidden() bool {
	return strings.EqualFold(strings.TrimSpace(c.Policy().FoundryAuthMode), FoundryAuthModeEntra)
}

// OllamaBaseURL is the Ollama endpoint the providers dial: the pinned
// Ollama\BaseURL when set (loopback only, see validateOllamaBaseURL), the
// configured base_url otherwise ("" keeps the adapter default).
func (c *Config) OllamaBaseURL() string {
	if c == nil {
		return ""
	}
	if pinned := strings.TrimSpace(c.Policy().OllamaBaseURL); pinned != "" {
		if configured := strings.TrimSpace(c.Providers.Ollama.BaseURL); configured != "" && configured != pinned {
			reportIgnoredEndpoint(`Ollama\BaseURL`, "providers.ollama.base_url")
		}
		return pinned
	}
	return strings.TrimSpace(c.Providers.Ollama.BaseURL)
}

// ProviderPolicyNotice is the start-up status line for a provider policy
// that holds back cloud providers, "" when nothing is held back. It names
// the reason (no machine policy in a managed host, or an empty managed
// allowlist) without listing provider settings.
func (c *Config) ProviderPolicyNotice() string {
	if c == nil || !ManagedMode() {
		return ""
	}
	if !c.HasMachinePolicy() {
		return "Policy: no administrator policy is installed; only on-device providers start [" + framework.PolicyReasonNoMachinePolicy + "]"
	}
	if len(c.providerAllowlist()) == 0 {
		return "Policy: the provider allowlist is empty; only on-device providers start [" + framework.PolicyReasonProviderNotAllowed + "]"
	}
	return ""
}

var ignoredEndpoints sync.Map

// reportIgnoredEndpoint logs once per process and key that a configured
// endpoint differs from the policy pin and is not used. The values stay out
// of the log.
func reportIgnoredEndpoint(policy, key string) {
	if _, seen := ignoredEndpoints.LoadOrStore(key, true); seen {
		return
	}
	slog.Warn("config: configured endpoint differs from the administrator policy pin and is ignored", "key", key)
	reportPolicyBlocked(policy, key)
}

// Policy names and actions of policy.blocked records raised here.
const (
	policyProvidersAllowed  = `Providers\Allowed`
	policyMachinePolicy     = "MachinePolicy"
	actionProviderConstruct = "provider.construct"
)

// PolicyBlock is one refusal reported through the hook set by
// SetPolicyBlockedHook.
type PolicyBlock struct {
	// Policy is the policy key name (`Section\Value`); "MachinePolicy" when a
	// managed host has no machine policy at all.
	Policy string
	// Action is what was refused, e.g. "provider.construct".
	Action string
	// Detail is a non-secret qualifier: a provider id or a config key.
	Detail string
}

var (
	policyBlockedMu   sync.Mutex
	policyBlockedHook func(PolicyBlock)
	policyBlockedSeen = map[PolicyBlock]bool{}
)

// SetPolicyBlockedHook registers fn to hear every provider-construction
// refusal, once per distinct (policy, action, detail) and process, so a
// refusal that is asked about repeatedly is recorded once. The composition
// root wires it to its audit trail. Setting a hook (nil clears it) forgets
// which refusals were already reported.
func SetPolicyBlockedHook(fn func(PolicyBlock)) {
	policyBlockedMu.Lock()
	defer policyBlockedMu.Unlock()
	policyBlockedHook = fn
	policyBlockedSeen = map[PolicyBlock]bool{}
}

func reportPolicyBlocked(policy, detail string) {
	b := PolicyBlock{Policy: policy, Action: actionProviderConstruct, Detail: detail}
	policyBlockedMu.Lock()
	fn := policyBlockedHook
	if fn == nil || policyBlockedSeen[b] {
		policyBlockedMu.Unlock()
		return
	}
	policyBlockedSeen[b] = true
	policyBlockedMu.Unlock()
	fn(b)
}

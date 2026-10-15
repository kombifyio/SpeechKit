package speechkit

// Stable, localizable disabled-reason IDs for the provider allowlist a host
// enforces at provider construction (the Providers\Allowed machine policy in
// the reference client and in managed hosts). They sit beside the
// NetworkScopeReason* and RetentionScopeReason* IDs; UIs render the matching
// catalog message rather than inventing copy.
const (
	// PolicyReasonProviderNotAllowed marks a provider that the provider
	// allowlist does not name, so it is never constructed.
	PolicyReasonProviderNotAllowed = "sk.policy.disabled.provider"
	// PolicyReasonNoMachinePolicy marks a cloud provider a managed host refuses
	// because no machine policy is present: without one a managed host starts
	// on-device providers only.
	PolicyReasonNoMachinePolicy = "sk.policy.disabled.no_machine_policy"
)

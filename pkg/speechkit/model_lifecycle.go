package speechkit

// ModelLifecycle classifies how a provider model ID should be treated by hosts:
// generally available, preview, legacy (still served but superseded) or
// deprecated (scheduled for removal). The catalog package assigns lifecycles to
// its model registry rows.
type ModelLifecycle string

const (
	ModelLifecycleGA         ModelLifecycle = "ga"
	ModelLifecyclePreview    ModelLifecycle = "preview"
	ModelLifecycleLegacy     ModelLifecycle = "legacy"
	ModelLifecycleDeprecated ModelLifecycle = "deprecated"
)

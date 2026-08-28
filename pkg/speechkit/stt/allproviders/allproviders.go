// Package allproviders is the batteries-included STT assembly layer: it knows
// every provider SpeechKit ships and turns a host's resolved credentials into
// a ready [stt.Router].
//
// Importing it compiles every provider, which is the right trade for a host
// that offers a provider choice at runtime (the desktop app, the server). A
// host that speaks to exactly one backend should import that provider's own
// package and assemble the router itself, and stays free of the others'
// dependencies.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package allproviders

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// RouterConfig carries the routing knobs a host resolves from its own config
// before delegating assembly to [BuildRouter].
type RouterConfig = stt.RouterConfig

// EnabledProviders carries the per-provider options a host has already
// resolved from its own config. Nil fields are skipped; Extra providers are
// appended after the named ones.
type EnabledProviders = stt.EnabledProviders

// Per-provider assembly options.
type (
	LocalOpts       = stt.LocalOpts
	VPSOpts         = stt.VPSOpts
	HuggingFaceOpts = stt.HuggingFaceOpts
	OpenAIOpts      = stt.OpenAIOpts
	GroqOpts        = stt.GroqOpts
	GoogleOpts      = stt.GoogleOpts
	DeepgramOpts    = stt.DeepgramOpts
	AssemblyAIOpts  = stt.AssemblyAIOpts
	OpenRouterOpts  = stt.OpenRouterOpts
	OllamaOpts      = stt.OllamaOpts
)

// BuildRouter assembles an STT router from the enabled providers in a stable
// cloud-fallback order, applies optional model_selection pinning, and returns
// the router plus human-readable notes. ok is false when nothing is enabled.
func BuildRouter(cfg RouterConfig, enabled EnabledProviders) (*stt.Router, bool, []string) {
	return stt.BuildRouter(cfg, enabled)
}

// BuildSpec carries the inputs needed to construct one cloud provider for a
// given execution mode.
type BuildSpec = stt.BuildSpec

// Build constructs the cloud provider for spec and returns its canonical name.
func Build(spec BuildSpec) (string, stt.STTProvider, error) { return stt.Build(spec) }

// Register adds a provider constructor under id so a host can extend [Build]
// with its own provider.
func Register(id, name string, build func(BuildSpec) (stt.STTProvider, error)) error {
	return stt.Register(id, name, build)
}

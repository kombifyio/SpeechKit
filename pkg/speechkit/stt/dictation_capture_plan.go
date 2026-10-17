package stt

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

// DictationBatchProvider resolves the same configured candidate/profile order
// as StartDictationStream without opening a transport. It returns a provider
// only when its known selected profile cannot stream a complete capture, so a
// finals-only host can transcribe that same provider's complete recording
// without an unnecessary live handshake. Unknown or complete-capture profiles
// retain normal startup; this is never a fallback after a provider failure.
func (r *Router) DictationBatchProvider(opts speechkit.DictationStreamOptions) (STTProvider, bool) {
	candidates := PrioritizeProviderProfile(r.dictationStreamingCandidates(opts.ProviderProfileID), opts.ProviderProfileID)
	if len(candidates) == 0 {
		return nil, false
	}
	provider := candidates[0]
	profile, known := catalog.DefaultCatalog().Profile(opts.ProviderProfileID)
	if !known {
		if strings.Contains(opts.ProviderProfileID, ".") {
			return nil, false
		}
		preferred, ok := catalog.FindProviderDefault(provider.Name(), speechkit.ModeDictation)
		if !ok {
			return nil, false
		}
		profile, known = catalog.DefaultCatalog().Profile(preferred.ProfileID)
	}
	if !known || profile.Mode != speechkit.ModeDictation || catalog.ProviderIDForProfile(profile) != catalog.NormalizeProviderID(provider.Name()) || profile.HasCapability(speechkit.CapabilityCompleteCaptureStream) {
		return nil, false
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		matches := profile.ModelID == model
		for _, supported := range strings.Split(profile.ModelID, ",") {
			matches = matches || strings.TrimSpace(supported) == model
		}
		if !matches {
			return nil, false
		}
	}
	return provider, true
}

package tts

import "strings"

// EnabledProviders carries the per-provider options a host has already resolved
// from its own config (env secrets, model/voice defaults). Nil fields are
// skipped. It lets the Device- and Server-Targets share one router-assembly
// path (BuildRouter) while each keeps its own config-resolution specifics.
type EnabledProviders struct {
	OpenAI      *OpenAIOpts
	Google      *GoogleOpts
	Deepgram    *DeepgramOpts
	HuggingFace *HuggingFaceOpts
	Foundry     *FoundryOpts
	Piper       *PiperOpts
	// PreferredProfileID optionally pins the provider matching this
	// model_selection profile to the front of the strategy order.
	PreferredProfileID string
}

// BuildRouter is the single source of truth for assembling a TTS router from a
// set of enabled providers: it constructs each enabled provider in a stable
// order, applies optional model_selection pinning, and returns the router plus
// human-readable notes. ok is false (router nil) when nothing is enabled.
func BuildRouter(strategy Strategy, enabled EnabledProviders) (router *Router, ok bool, notes []string) {
	var providers []Provider

	if enabled.OpenAI != nil {
		providers = append(providers, NewOpenAI(*enabled.OpenAI))
		notes = append(notes, "TTS: OpenAI registered (voice="+enabled.OpenAI.Voice+")")
	}
	if enabled.Google != nil {
		providers = append(providers, NewGoogle(*enabled.Google))
		notes = append(notes, "TTS: Google registered (voice="+enabled.Google.Voice+")")
	}
	if enabled.Deepgram != nil {
		providers = append(providers, NewDeepgram(*enabled.Deepgram))
		notes = append(notes, "TTS: Deepgram Aura registered (model="+enabled.Deepgram.Model+")")
	}
	if enabled.HuggingFace != nil {
		providers = append(providers, NewHuggingFace(*enabled.HuggingFace))
		notes = append(notes, "TTS: HuggingFace registered")
	}
	if enabled.Foundry != nil {
		if strings.TrimSpace(enabled.Foundry.BaseURL) != "" {
			providers = append(providers, NewFoundry(*enabled.Foundry))
			notes = append(notes, "TTS: Microsoft Foundry registered (deployment="+enabled.Foundry.Model+", voice="+enabled.Foundry.Voice+")")
		} else {
			notes = append(notes, "TTS: Microsoft Foundry skipped (project endpoint missing)")
		}
	}
	if enabled.Piper != nil {
		piper, err := NewPiper(*enabled.Piper)
		if err != nil {
			notes = append(notes, "TTS: Piper init failed: "+err.Error())
		} else {
			providers = append(providers, piper)
			notes = append(notes, "TTS: Piper registered (voice_dir="+strings.TrimSpace(enabled.Piper.VoiceDir)+")")
		}
	}

	if len(providers) == 0 {
		return nil, false, notes
	}

	if profileID := strings.TrimSpace(enabled.PreferredProfileID); profileID != "" {
		if preferred := PreferredProviderForProfileID(profileID); preferred != "" {
			ordered := OrderByPreferredProvider(providers, preferred)
			if len(ordered) > 0 && ordered[0].Name() == preferred {
				notes = append(notes, "TTS: model_selection profile "+profileID+" pinned provider "+preferred+" first")
			} else {
				notes = append(notes, "TTS: model_selection profile "+profileID+" requested provider "+preferred+" but it is not configured; using strategy order")
			}
			providers = ordered
		}
	}

	return NewRouter(strategy, providers...), true, notes
}

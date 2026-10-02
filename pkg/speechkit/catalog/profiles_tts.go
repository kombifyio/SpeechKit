package catalog

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ttsProviderProfiles returns the built-in TTS (voice output) section of the
// default provider catalog.
func ttsProviderProfiles() []speechkit.ProviderProfile {
	return []speechkit.ProviderProfile{
		// ─── TTS (Voice Output) profile catalog ─────────────────────────────
		// Added in v0.37 alongside the hands-free Voice-Companion flow so
		// hosts pin a stable TTS voice per deployment ("Thalia speaks via
		// Studio-O", "Companion Live via OpenAI gpt-4o-mini-tts Nova"). The four
		// ProviderKind groups follow the same V23 invariant as the other
		// modes: Local Built-in (Piper, shipped Phase 3), Local Provider
		// (OpenAI-compatible self-hosted via Kokoro/openedai-speech),
		// Cloud Provider (Hugging Face), Direct Provider (OpenAI + Google).
		// Kokoro-82M is the current Hugging Face TTS leader (68M downloads,
		// 6.1k likes as of 2026-05). Apache-2.0, ~82M parameters, ONNX
		// export available (~50 MB int8 / ~310 MB fp32). Best-in-class
		// English quality at very low compute. Bundled into the v0.37
		// installer as the recommended Local Built-in default. Phase-3
		// runtime: kokoro-onnx via onnxruntime-go subprocess wrapper.
		{
			ID:            "tts.local.kokoro-82m",
			Mode:          speechkit.ModeTTS,
			Name:          "Kokoro 82M (Local Built-in, recommended)",
			ProviderKind:  speechkit.ProviderKindLocalBuiltIn,
			ExecutionMode: speechkit.ExecutionModeLocal,
			ModelID:       "hexgrad/Kokoro-82M",
			Source:        "Local Built-in",
			Description:   "StyleTTS2-based Kokoro 82M. Apache-2.0, 68M+ Hugging Face downloads, ~50 MB ONNX (int8) — the recommended Local Built-in default for v0.37+. English voices ship in the installer; community v0.19/v1 forks add JP/DE/CN. Phase-3 runtime via onnxruntime-go sidecar.",
			License:       "apache-2.0",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "kokoro_local",
			Variants: []speechkit.ModelVariant{
				{ID: "kokoro.en.af-bella", Name: "Bella (EN, female)", ModelID: "kokoro-v1_0|af_bella", Recommended: true},
				{ID: "kokoro.en.af-nova", Name: "Nova (EN, female)", ModelID: "kokoro-v1_0|af_nova"},
				{ID: "kokoro.en.am-michael", Name: "Michael (EN, male)", ModelID: "kokoro-v1_0|am_michael"},
				{ID: "kokoro.en.bf-emma", Name: "Emma (BR-EN, female)", ModelID: "kokoro-v1_0|bf_emma"},
			},
			AllowInference: false,
			Default:        true,
			Recommended:    true,
			Experimental:   true,
		},
		// Supertonic-3 is the most-trended Hugging Face TTS model of
		// May 2026 (trending score 331). OpenRAIL license, ONNX, true
		// multilingual coverage (32 languages including DE/EN/JA/AR/KO).
		// Optimised for on-device inference. Phase-3 runtime via the
		// upstream `supertonic` Python library bridged through the
		// same sidecar pattern as Kokoro.
		{
			ID:            "tts.local.supertonic-3",
			Mode:          speechkit.ModeTTS,
			Name:          "Supertonic-3 (Local Built-in, multilingual)",
			ProviderKind:  speechkit.ProviderKindLocalBuiltIn,
			ExecutionMode: speechkit.ExecutionModeLocal,
			ModelID:       "Supertone/supertonic-3",
			Source:        "Local Built-in",
			Description:   "Supertonic-3 multilingual on-device TTS. OpenRAIL license, 32 languages with strong DE/EN/JA/AR/KO coverage. ONNX. Trending #3 on Hugging Face TTS leaderboard (May 2026). Phase-3 runtime via onnx sidecar.",
			License:       "openrail",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "supertonic_local",
			Variants: []speechkit.ModelVariant{
				{ID: "supertonic3.de.default", Name: "Standard (DE)", ModelID: "supertonic-3|de", Recommended: true},
				{ID: "supertonic3.en.default", Name: "Standard (EN)", ModelID: "supertonic-3|en"},
				{ID: "supertonic3.multilingual", Name: "Auto-language", ModelID: "supertonic-3|auto"},
			},
			AllowInference: false,
			Recommended:    true,
			Experimental:   true,
		},
		// Chatterbox-multilingual is the strongest open voice-cloning
		// TTS on Hugging Face. Same ONNX path as Kokoro/Supertonic.
		// Voice-cloning capability is the differentiator — useful for
		// assistant personas that need a custom voice baked from a short
		// reference clip.
		{
			ID:            "tts.local.chatterbox-multilingual",
			Mode:          speechkit.ModeTTS,
			Name:          "Chatterbox Multilingual (Local Built-in, voice-clone)",
			ProviderKind:  speechkit.ProviderKindLocalBuiltIn,
			ExecutionMode: speechkit.ExecutionModeLocal,
			ModelID:       "onnx-community/chatterbox-multilingual-ONNX",
			Source:        "Local Built-in",
			Description:   "Chatterbox multilingual TTS with voice-cloning support. 24 languages (DE/EN/ES/FR/IT/JA/KO/...). ONNX, on-device. Phase-3 runtime via the same sidecar pattern as Kokoro; voice-clone reference clip configurable per Persona.",
			License:       "mit",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "chatterbox_local",
			Variants: []speechkit.ModelVariant{
				{ID: "chatterbox.de.default", Name: "Standard (DE)", ModelID: "chatterbox|de"},
				{ID: "chatterbox.en.default", Name: "Standard (EN)", ModelID: "chatterbox|en", Recommended: true},
				{ID: "chatterbox.clone", Name: "Custom voice clone", ModelID: "chatterbox|cloned"},
			},
			AllowInference: false,
			Experimental:   true,
		},
		// Piper retained as an additional Local Built-in option for the
		// Home-Assistant-aligned crowd — Piper is HA Voice's canonical
		// engine and many users have ready-made voice catalogs.
		{
			ID:            "tts.local.piper",
			Mode:          speechkit.ModeTTS,
			Name:          "Piper Local TTS (HA-compatible voices)",
			ProviderKind:  speechkit.ProviderKindLocalBuiltIn,
			ExecutionMode: speechkit.ExecutionModeLocal,
			Provider:      "piper",
			ModelID:       "rhasspy/piper",
			Source:        "Local Built-in",
			Description:   "Piper offline neural TTS — the canonical Home Assistant Voice engine. MIT-licensed, ~50 MB per voice, broad multilingual voice catalog. Runs through the SpeechKit piper subprocess adapter; configure binary and voice directory under [tts.piper].",
			License:       "mit",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "piper_local",
			Variants: []speechkit.ModelVariant{
				{ID: "piper.de.thorsten-medium", Name: "Thorsten (DE, Medium)", ModelID: "de_DE-thorsten-medium.onnx", Recommended: true},
				{ID: "piper.de.thorsten-high", Name: "Thorsten (DE, High)", ModelID: "de_DE-thorsten-high.onnx"},
				{ID: "piper.en.amy-medium", Name: "Amy (EN, Medium)", ModelID: "en_US-amy-medium.onnx"},
				{ID: "piper.en.lessac-medium", Name: "Lessac (EN, Medium)", ModelID: "en_US-lessac-medium.onnx"},
			},
			AllowInference: true,
		},
		{
			ID:             "tts.openedai.kokoro",
			Mode:           speechkit.ModeTTS,
			Name:           "Kokoro 82M (OpenAI-compatible local)",
			ProviderKind:   speechkit.ProviderKindLocalProvider,
			ExecutionMode:  speechkit.ExecutionModeSelfHostedHTTP,
			ModelID:        "kokoro-82m",
			Source:         "Local Provider",
			Description:    "Self-hosted Kokoro-82M TTS exposed through an OpenAI-compatible /v1/audio/speech endpoint (openedai-speech / similar). Configure URL + API key under [tts] manually until the readiness wizard lands.",
			License:        "apache-2.0",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:    "openai_compatible_tts",
			AllowInference: true,
			Experimental:   true,
		},
		{
			ID:             "tts.huggingface.parler-multilingual",
			Mode:           speechkit.ModeTTS,
			Name:           "Parler-TTS Mini Multilingual (Hugging Face)",
			ProviderKind:   speechkit.ProviderKindCloudProvider,
			ExecutionMode:  speechkit.ExecutionModeHFRouted,
			ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-Base",
			Source:         "Hugging Face",
			Description:    "Hugging Face Inference Router serves the Parler multilingual voice. Requires an HF token. Good baseline cloud option without OpenAI / Google credentials.",
			License:        "apache-2.0",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:    "hf_tts",
			AllowInference: true,
		},
		{
			ID:            "tts.google.studio-o-de",
			Mode:          speechkit.ModeTTS,
			Name:          "Google Cloud Text-to-Speech (Chirp 3 HD, Studio)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeGoogle,
			ModelID:       "de-DE-Chirp3-HD-Kore",
			Source:        "Google",
			Description:   "Opt-in Google Cloud Text-to-Speech with your own Google API key (Text-to-Speech API enabled). Chirp 3 HD voices are Google's current low-latency generation; the Studio and WaveNet voices remain selectable. The profile id keeps its historical name. Never a default; the local Kokoro profile stays the Voice-Output default.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "google_tts",
			EvidenceURL:   "https://docs.cloud.google.com/text-to-speech/docs/chirp3-hd",
			Variants: []speechkit.ModelVariant{
				{ID: "google.de.chirp3-hd-kore", Name: "Chirp 3 HD Kore (DE, female)", ModelID: "de-DE-Chirp3-HD-Kore", Recommended: true},
				{ID: "google.de.chirp3-hd-charon", Name: "Chirp 3 HD Charon (DE, male)", ModelID: "de-DE-Chirp3-HD-Charon"},
				{ID: "google.en.chirp3-hd-kore", Name: "Chirp 3 HD Kore (EN-US, female)", ModelID: "en-US-Chirp3-HD-Kore"},
				{ID: "google.en.chirp3-hd-charon", Name: "Chirp 3 HD Charon (EN-US, male)", ModelID: "en-US-Chirp3-HD-Charon"},
				{ID: "google.de.studio-o", Name: "Studio-O (DE)", ModelID: "de-DE-Studio-O"},
				{ID: "google.de.studio-q", Name: "Studio-Q (DE)", ModelID: "de-DE-Studio-Q"},
				{ID: "google.de.wavenet-f", Name: "Wavenet-F (DE)", ModelID: "de-DE-Wavenet-F"},
				{ID: "google.en.studio-o", Name: "Studio-O (EN-US)", ModelID: "en-US-Studio-O"},
			},
			AllowInference: true,
		},
		{
			// Gemini API speech rides the Google voice-output switch and the
			// Gemini key; ttsroute maps this id to the "gemini" provider.
			ID:            "tts.google.gemini-3.8-tts",
			Mode:          speechkit.ModeTTS,
			Name:          "Gemini 3.8 TTS (Google)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeGoogle,
			Provider:      "google",
			ModelID:       modelGemini38FlashLiteTTS,
			Lifecycle:     speechkit.ModelLifecycleGA,
			Source:        "Google Gemini API",
			Description:   "Opt-in Gemini 3.8 speech with your own GOOGLE_AI_API_KEY: 30 prebuilt voices that speak 100+ languages with automatic language detection. Flash-Lite is the fast default, Flash the higher-quality flagship. Never a default.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "gemini_tts",
			EvidenceURL:   "https://ai.google.dev/gemini-api/docs/models/gemini-3.8-flash-lite-tts",
			Variants: []speechkit.ModelVariant{
				{ID: "gemini.tts.flash-lite.kore", Name: "Kore (Flash-Lite)", ModelID: modelGemini38FlashLiteTTS + "|Kore", Recommended: true},
				{ID: "gemini.tts.flash-lite.charon", Name: "Charon (Flash-Lite)", ModelID: modelGemini38FlashLiteTTS + "|Charon"},
				{ID: "gemini.tts.flash.kore", Name: "Kore (Flash)", ModelID: modelGemini38FlashTTS + "|Kore", Description: "Flagship quality with richer delivery control."},
				{ID: "gemini.tts.flash.puck", Name: "Puck (Flash)", ModelID: modelGemini38FlashTTS + "|Puck"},
			},
			AllowInference: true,
			Experimental:   true,
		},
		{
			ID:            "tts.deepgram.aura-2",
			Mode:          speechkit.ModeTTS,
			Name:          "Deepgram Aura-2",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeDeepgram,
			ModelID:       "aura-2-thalia-en",
			Source:        "Deepgram",
			Description:   "Deepgram Aura-2 low-latency neural TTS. Recommended Voice-Output path for kombify reference deployments; English + German voices over the single DEEPGRAM_API_KEY shared with Deepgram STT and the Voice Agent.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "deepgram_tts",
			Variants: []speechkit.ModelVariant{
				{ID: "deepgram.aura2.thalia-en", Name: "Thalia (EN, female)", ModelID: "aura-2-thalia-en", Recommended: true},
				{ID: "deepgram.aura2.apollo-en", Name: "Apollo (EN, male)", ModelID: "aura-2-apollo-en"},
				{ID: "deepgram.aura2.viktoria-de", Name: "Viktoria (DE, female)", ModelID: "aura-2-viktoria-de"},
				{ID: "deepgram.aura2.julius-de", Name: "Julius (DE, male)", ModelID: "aura-2-julius-de"},
				{ID: "deepgram.aura2.elara-de", Name: "Elara (DE, female)", ModelID: "aura-2-elara-de"},
				{ID: "deepgram.aura2.fabian-de", Name: "Fabian (DE, male)", ModelID: "aura-2-fabian-de"},
				{ID: "deepgram.aura2.lara-de", Name: "Lara (DE, female)", ModelID: "aura-2-lara-de"},
				{ID: "deepgram.flux.kit-en", Name: "Kit (EN, Flux TTS)", ModelID: modelDeepgramFluxTTSDefaultEN, Description: "Flux TTS (GA 2026-08-12): conversation-native English voice over the v2 streaming leg."},
				{ID: "deepgram.flux.alexis-en", Name: "Alexis (EN, Flux TTS)", ModelID: "flux-alexis-en", Description: "Flux TTS English voice."},
				{ID: "deepgram.flux.haley-en", Name: "Haley (EN, Flux TTS)", ModelID: "flux-haley-en", Description: "Flux TTS English voice."},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:            "tts.openai.gpt-4o-mini-tts",
			Mode:          speechkit.ModeTTS,
			Name:          "OpenAI GPT-4o mini TTS",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeOpenAI,
			ModelID:       modelOpenAIGPT4OMiniTTS,
			Source:        "OpenAI",
			Description:   "OpenAI's current text-to-speech model with 13 built-in voices, including marin and cedar. It replaces tts-1 and tts-1-hd. Requires an OpenAI API key.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "openai_tts",
			EvidenceURL:   "https://developers.openai.com/api/docs/models/gpt-4o-mini-tts",
			Variants: []speechkit.ModelVariant{
				{ID: "openai.gpt-4o-mini-tts.nova", Name: "Nova", ModelID: modelOpenAIGPT4OMiniTTS + "|nova", Recommended: true},
				{ID: "openai.gpt-4o-mini-tts.marin", Name: "Marin", ModelID: modelOpenAIGPT4OMiniTTS + "|marin"},
				{ID: "openai.gpt-4o-mini-tts.cedar", Name: "Cedar", ModelID: modelOpenAIGPT4OMiniTTS + "|cedar"},
				{ID: "openai.gpt-4o-mini-tts.coral", Name: "Coral", ModelID: modelOpenAIGPT4OMiniTTS + "|coral"},
				{ID: "openai.gpt-4o-mini-tts.shimmer", Name: "Shimmer", ModelID: modelOpenAIGPT4OMiniTTS + "|shimmer"},
				{ID: "openai.gpt-4o-mini-tts.alloy", Name: "Alloy", ModelID: modelOpenAIGPT4OMiniTTS + "|alloy"},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			// MAI-Voice-2 voices are Azure Speech voices on the same Foundry
			// resource (SSML on the Cognitive Services host), not audio/speech
			// deployments. Variant ids follow the "<family>|<voice>" shape of
			// the other TTS profiles; the voice is a Speech short name.
			ID:            "tts.foundry.mai-voice-2",
			Mode:          speechkit.ModeTTS,
			Name:          "MAI-Voice-2 (Microsoft Foundry)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeFoundry,
			Provider:      "foundry",
			ModelID:       "MAI-Voice-2",
			Lifecycle:     speechkit.ModelLifecyclePreview,
			Source:        "Microsoft Foundry",
			Description:   "Microsoft's expressive text to speech through Azure Speech on the Foundry resource. MAI-Voice-2 is the high-fidelity voice family, MAI-Voice-2-Flash the low-latency one for agents; both offer 18 locales and emotion styles. Public preview.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "foundry_tts",
			EvidenceURL:   "https://learn.microsoft.com/azure/ai-services/speech-service/mai-voices",
			Variants: []speechkit.ModelVariant{
				{ID: "foundry.mai-voice-2.de-mia", Name: "Mia, German (MAI-Voice-2)", ModelID: "MAI-Voice-2|de-DE-Mia:MAI-Voice-2", Recommended: true},
				{ID: "foundry.mai-voice-2.de-klaus", Name: "Klaus, German (MAI-Voice-2)", ModelID: "MAI-Voice-2|de-DE-Klaus:MAI-Voice-2"},
				{ID: "foundry.mai-voice-2.en-harper", Name: "Harper, English US (MAI-Voice-2)", ModelID: "MAI-Voice-2|en-US-Harper:MAI-Voice-2"},
				{ID: "foundry.mai-voice-2.en-ethan", Name: "Ethan, English US (MAI-Voice-2)", ModelID: "MAI-Voice-2|en-US-Ethan:MAI-Voice-2"},
				{ID: "foundry.mai-voice-2-flash.de-mia", Name: "Mia, German (MAI-Voice-2-Flash)", ModelID: "MAI-Voice-2-Flash|de-DE-Mia:MAI-Voice-2-Flash", Description: "Low-latency variant for Voice Agent read-back."},
				{ID: "foundry.mai-voice-2-flash.en-ethan", Name: "Ethan, English US (MAI-Voice-2-Flash)", ModelID: "MAI-Voice-2-Flash|en-US-Ethan:MAI-Voice-2-Flash", Description: "Low-latency variant for Voice Agent read-back."},
			},
			AllowInference: true,
			Recommended:    true,
			Experimental:   true,
		},
	}
}

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
		// Studio-O", "Companion Live via OpenAI tts-1-hd Nova"). The four
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
		// kombify Companion personas that need a custom voice baked
		// from a short reference clip.
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
			Name:          "Google Studio-O (DE-DE)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeGoogle,
			ModelID:       "de-DE-Studio-O",
			Source:        "Google",
			Description:   "Opt-in Google Cloud Text-to-Speech Studio voices with your own Google API key (Text-to-Speech API enabled). Never a default; the local Kokoro profile stays the Voice-Output default.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "google_tts",
			Variants: []speechkit.ModelVariant{
				{ID: "google.de.studio-o", Name: "Studio-O (DE)", ModelID: "de-DE-Studio-O", Recommended: true},
				{ID: "google.de.studio-q", Name: "Studio-Q (DE)", ModelID: "de-DE-Studio-Q"},
				{ID: "google.de.wavenet-f", Name: "Wavenet-F (DE)", ModelID: "de-DE-Wavenet-F"},
				{ID: "google.en.studio-o", Name: "Studio-O (EN-US)", ModelID: "en-US-Studio-O"},
			},
			AllowInference: true,
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
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:            "tts.openai.tts-1-hd",
			Mode:          speechkit.ModeTTS,
			Name:          "OpenAI TTS-1-HD",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeOpenAI,
			ModelID:       "tts-1-hd",
			Source:        "OpenAI",
			Description:   "OpenAI's high-definition TTS. Pricier than tts-1 but higher voice quality. Six built-in voices (alloy, echo, fable, onyx, nova, shimmer). Requires an OpenAI API key.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityTTS},
			AdapterKind:   "openai_tts",
			Variants: []speechkit.ModelVariant{
				{ID: "openai.tts1hd.nova", Name: "Nova (HD)", ModelID: "tts-1-hd|nova", Recommended: true},
				{ID: "openai.tts1hd.shimmer", Name: "Shimmer (HD)", ModelID: "tts-1-hd|shimmer"},
				{ID: "openai.tts1hd.alloy", Name: "Alloy (HD)", ModelID: "tts-1-hd|alloy"},
				{ID: "openai.tts1.nova", Name: "Nova (Standard)", ModelID: "tts-1|nova"},
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

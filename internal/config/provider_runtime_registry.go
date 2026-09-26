package config

import (
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

var providerRuntimeRegistry = []ProviderRuntime{
	{
		Provider:         "local",
		DisplayName:      "Local Built-in",
		ProviderKind:     framework.ProviderKindLocalBuiltIn,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable: false,
	},
	{
		Provider:         "ollama",
		DisplayName:      "Ollama",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SetupURL:         "https://ollama.com/download",
		SupportedModes:   []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable: true,
	},
	{
		Provider:           "huggingface",
		DisplayName:        "Hugging Face",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "huggingface",
		CredentialRequired: true,
		SetupURL:           "https://huggingface.co/settings/tokens",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "openrouter",
		DisplayName:        "OpenRouter",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "openrouter",
		CredentialRequired: true,
		SetupURL:           "https://openrouter.ai/settings/keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:           "openai",
		DisplayName:        "OpenAI",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "openai",
		CredentialRequired: true,
		SetupURL:           "https://platform.openai.com/api-keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		// Google is an opt-in BYOK provider: Cloud Speech-to-Text, Cloud
		// Text-to-Speech and Gemini Live with the user's own credentials.
		// It is never a default and Kombify's own deployments do not select it.
		Provider:           "google",
		DisplayName:        "Google Cloud / Gemini",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "google",
		CredentialRequired: true,
		SetupURL:           "https://aistudio.google.com/apikey",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "groq",
		DisplayName:        "Groq",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "groq",
		CredentialRequired: true,
		SetupURL:           "https://console.groq.com/keys",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist},
		UserConfigurable:   true,
	},
	{
		Provider:           "deepgram",
		DisplayName:        "Deepgram",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "deepgram",
		CredentialRequired: true,
		SetupURL:           "https://console.deepgram.com/",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		Provider:           "assemblyai",
		DisplayName:        "AssemblyAI",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "assemblyai",
		CredentialRequired: true,
		SetupURL:           "https://www.assemblyai.com/app/account",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:           "cloudflare",
		DisplayName:        "Cloudflare AI Gateway",
		ProviderKind:       framework.ProviderKindCloudProvider,
		IntegrationKind:    ProviderIntegrationCloudGateway,
		CredentialTarget:   "cloudflare",
		CredentialRequired: true,
		SetupURL:           "https://developers.cloudflare.com/ai-gateway/get-started/",
		SupportedModes:     []framework.Mode{framework.ModeAssist, framework.ModeVoiceAgent},
		UserConfigurable:   true,
	},
	{
		Provider:           "foundry",
		DisplayName:        "Microsoft Foundry",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "foundry",
		CredentialRequired: true,
		SetupURL:           "https://ai.azure.com",
		SupportedModes:     []framework.Mode{framework.ModeDictation, framework.ModeAssist, framework.ModeVoiceAgent, framework.ModeTTS},
		UserConfigurable:   true,
	},
	{
		// Voice Live is a second realtime surface of the same Foundry
		// resource: it shares the [providers.foundry] credential, endpoint
		// and enable flag, so it is not user-configurable on its own.
		Provider:           "foundry-voicelive",
		DisplayName:        "Microsoft Foundry Voice Live",
		ProviderKind:       framework.ProviderKindDirectProvider,
		IntegrationKind:    ProviderIntegrationDirectAPI,
		CredentialTarget:   "foundry",
		CredentialRequired: true,
		SetupURL:           "https://ai.azure.com",
		SupportedModes:     []framework.Mode{framework.ModeVoiceAgent},
		UserConfigurable:   false,
	},
	{
		// Piper is in the framework catalog as a Local Built-in TTS engine
		// and had no runtime row, which is what
		// TestProviderRuntimeRegistryCoversFrameworkMatrix has been failing
		// on: Settings could not describe a provider the catalog offers.
		// It needs no credential — the operator supplies a binary and voice
		// files through [tts.piper] — so it is configurable without being
		// credentialed.
		Provider:         "piper",
		DisplayName:      "Piper Local TTS",
		ProviderKind:     framework.ProviderKindLocalBuiltIn,
		IntegrationKind:  ProviderIntegrationLocal,
		SetupURL:         "https://github.com/rhasspy/piper",
		SupportedModes:   []framework.Mode{framework.ModeTTS},
		UserConfigurable: true,
	},
	{
		Provider:         "openedai",
		DisplayName:      "OpenAI-compatible local",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeTTS},
		UserConfigurable: false,
	},
	{
		Provider:         "selfhosted",
		DisplayName:      "Self-hosted HTTP",
		ProviderKind:     framework.ProviderKindLocalProvider,
		IntegrationKind:  ProviderIntegrationLocal,
		SupportedModes:   []framework.Mode{framework.ModeDictation},
		UserConfigurable: false,
	},
}

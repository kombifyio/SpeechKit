package config

import "testing"

func TestFoundryEndpointDerivation(t *testing.T) {
	const project = "https://myres.services.ai.azure.com/api/projects/dss"

	tests := []struct {
		name string
		fn   func(string) (string, error)
		in   string
		want string
	}{
		{"inference base", FoundryInferenceBase, project, "https://myres.services.ai.azure.com/openai/v1"},
		{"mai base", FoundryMAIInferenceBase, project, "https://myres.services.ai.azure.com/mai/v1"},
		{"realtime", FoundryRealtimeURL, project, "wss://myres.services.ai.azure.com/openai/v1/realtime"},
		{"voice live", FoundryVoiceLiveURL, project, "wss://myres.services.ai.azure.com/voice-live/realtime"},
		{"deployments", FoundryProjectDeploymentsURL, project, "https://myres.services.ai.azure.com/api/projects/dss/deployments?api-version=v1"},
		{"deployments trailing slash", FoundryProjectDeploymentsURL, project + "/", "https://myres.services.ai.azure.com/api/projects/dss/deployments?api-version=v1"},
		{"speech host from services", FoundrySpeechHost, project, "myres.cognitiveservices.azure.com"},
		{"speech host from openai", FoundrySpeechHost, "https://myres.openai.azure.com", "myres.cognitiveservices.azure.com"},
		{"speech host already cognitive", FoundrySpeechHost, "https://myres.cognitiveservices.azure.com/", "myres.cognitiveservices.azure.com"},
		{"speech host custom kept", FoundrySpeechHost, "https://speech.contoso.internal", "speech.contoso.internal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFoundryProjectDeploymentsURLRequiresProject(t *testing.T) {
	for _, in := range []string{
		"https://myres.services.ai.azure.com",
		"https://myres.openai.azure.com/",
		"https://myres.services.ai.azure.com/api/projects/",
	} {
		if _, err := FoundryProjectDeploymentsURL(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestFoundryEndpointRejectsInsecure(t *testing.T) {
	for _, in := range []string{"", "http://myres.services.ai.azure.com", "ftp://x"} {
		if _, err := FoundryInferenceHost(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestFoundryEngines(t *testing.T) {
	var cfg FoundryProviderConfig
	if got := cfg.STTEngine(); got != FoundryEngineOpenAI {
		t.Fatalf("default STT engine = %q", got)
	}
	if got := cfg.TTSEngine(); got != FoundryEngineOpenAI {
		t.Fatalf("default TTS engine = %q", got)
	}

	cfg.STTDeployment = "mai-transcribe-2"
	if got := cfg.STTEngine(); got != FoundryEngineSpeech {
		t.Fatalf("MAI STT engine = %q", got)
	}

	cfg.TTSDeployment = "MAI-Voice-2-Flash"
	cfg.TTSVoice = "alloy" // stale OpenAI voice must not reach the Speech API
	if got := cfg.TTSEngine(); got != FoundryEngineSpeech {
		t.Fatalf("MAI TTS engine = %q", got)
	}
	if got := cfg.ResolvedTTSVoice(); got != "en-US-Harper:MAI-Voice-2-Flash" {
		t.Fatalf("flash default voice = %q", got)
	}
	cfg.TTSVoice = "de-DE-Mia:MAI-Voice-2"
	if got := cfg.ResolvedTTSVoice(); got != "de-DE-Mia:MAI-Voice-2" {
		t.Fatalf("explicit voice = %q", got)
	}

	cfg.TTSDeployment = "gpt-4o-mini-tts"
	cfg.TTSVoice = ""
	if got := cfg.ResolvedTTSVoice(); got != DefaultFoundryTTSVoice {
		t.Fatalf("openai default voice = %q", got)
	}
}

func TestFoundryAuthResolution(t *testing.T) {
	var cfg FoundryProviderConfig
	if cfg.UsesEntra() {
		t.Fatal("default must be the api key path")
	}
	cfg.AuthMode = "Entra"
	if !cfg.UsesEntra() {
		t.Fatal("entra auth mode not recognised")
	}
	for in, want := range map[string]string{
		"":            FoundryEntraCredentialAuto,
		"azure_cli":   FoundryEntraCredentialAzureCLI,
		"az":          FoundryEntraCredentialAzureCLI,
		"browser":     FoundryEntraCredentialBrowser,
		"device-code": FoundryEntraCredentialDeviceCode,
		"bogus":       FoundryEntraCredentialAuto,
	} {
		cfg.EntraCredential = in
		if got := cfg.ResolvedEntraCredential(); got != want {
			t.Fatalf("credential %q → %q, want %q", in, got, want)
		}
	}
	cfg.AzureCLIProfile = "ISOLATED"
	if got := cfg.ResolvedAzureCLIProfile(); got != FoundryAzureCLIProfileIsolated {
		t.Fatalf("profile = %q", got)
	}
	cfg.STTStyle = "VERBATIM"
	if got := cfg.ResolvedSTTStyle(); got != "verbatim" {
		t.Fatalf("style = %q", got)
	}
	cfg.VoiceLiveTranscription = "MAI-Transcribe-2"
	if got := cfg.ResolvedVoiceLiveTranscription(); got != "mai-transcribe-2" {
		t.Fatalf("voice live transcription = %q", got)
	}
}

func TestIsMAIModelHelpers(t *testing.T) {
	if !IsMAITranscribeModel("MAI-Transcribe-1.5") || IsMAITranscribeModel("gpt-4o-transcribe") {
		t.Fatal("IsMAITranscribeModel")
	}
	if !IsMAIVoiceModel("mai-voice-2") || !IsMAIVoiceModel("de-DE-Klaus:MAI-Voice-2-Flash") || IsMAIVoiceModel("alloy") {
		t.Fatal("IsMAIVoiceModel")
	}
	if !IsMAIThinkingModel("MAI-Thinking-1") || IsMAIThinkingModel("gpt-5.1") {
		t.Fatal("IsMAIThinkingModel")
	}
}

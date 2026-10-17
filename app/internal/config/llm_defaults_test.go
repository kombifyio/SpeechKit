package config

import "testing"

func TestApplyAssemblyAILLMDefaultsFillsGatewayWhenEnabled(t *testing.T) {
	cfg := &Config{}
	cfg.Providers.AssemblyAI.Enabled = true

	ApplyAssemblyAILLMDefaults(cfg)

	if cfg.Providers.AssemblyAI.STTModels != DefaultAssemblyAISTTModels {
		t.Fatalf("STT models = %q", cfg.Providers.AssemblyAI.STTModels)
	}
	if cfg.Providers.AssemblyAI.StreamingModel != DefaultAssemblyAIStreamingModel {
		t.Fatalf("streaming model = %q", cfg.Providers.AssemblyAI.StreamingModel)
	}
	if cfg.Providers.AssemblyAI.LLMGatewayUtilityModel != DefaultAssemblyAILLMGatewayUtilityModel {
		t.Fatalf("utility model = %q", cfg.Providers.AssemblyAI.LLMGatewayUtilityModel)
	}
	if cfg.Providers.AssemblyAI.LLMGatewayAgentModel != DefaultAssemblyAILLMGatewayAgentModel {
		t.Fatalf("agent model = %q", cfg.Providers.AssemblyAI.LLMGatewayAgentModel)
	}
	if !cfg.Providers.AssemblyAI.StreamingLLM {
		t.Fatal("streaming LLM should be on while AssemblyAI is enabled")
	}
}

func TestApplyAssemblyAILLMDefaultsUpgradesLegacyFlagship(t *testing.T) {
	cfg := &Config{}
	cfg.Providers.AssemblyAI.Enabled = true
	cfg.Providers.AssemblyAI.STTModels = "universal-3-pro,universal-2"
	cfg.Providers.AssemblyAI.StreamingModel = "u3-rt-pro"

	ApplyAssemblyAILLMDefaults(cfg)

	if cfg.Providers.AssemblyAI.STTModels != DefaultAssemblyAISTTModels {
		t.Fatalf("STT models = %q, want flagship upgrade", cfg.Providers.AssemblyAI.STTModels)
	}
	if cfg.Providers.AssemblyAI.StreamingModel != DefaultAssemblyAIStreamingModel {
		t.Fatalf("streaming model = %q, want flagship upgrade", cfg.Providers.AssemblyAI.StreamingModel)
	}
}

func TestApplyAssemblyAILLMDefaultsSkipsWhenDisabled(t *testing.T) {
	cfg := &Config{}
	cfg.VoiceAgent.Provider = "openai"
	ApplyAssemblyAILLMDefaults(cfg)
	if cfg.Providers.AssemblyAI.StreamingLLM {
		t.Fatal("disabled AssemblyAI should not force streaming LLM")
	}
}

func TestEnableAlwaysOnLLMTurnsOnCloudflareWhenCloudConnected(t *testing.T) {
	cfg := &Config{}
	cfg.ServerConnection.Enabled = true
	cfg.ServerConnection.URL = "https://speechkit.kombify.io"

	EnableAlwaysOnLLM(cfg)

	if !cfg.Providers.Cloudflare.Enabled {
		t.Fatal("kombify Cloud should enable Cloudflare AI Gateway")
	}
	if cfg.Providers.Cloudflare.UtilityModel != DefaultCloudflareAIGatewayUtilityModel {
		t.Fatalf("cloudflare utility model = %q", cfg.Providers.Cloudflare.UtilityModel)
	}
}

func TestEnableAlwaysOnLLMTurnsOnCloudflareWhenCredentialsResolve(t *testing.T) {
	t.Setenv(CloudflareAIGatewayAuthTokenEnv, "cf-token")
	t.Setenv(CloudflareAccountIDEnv, "cf-account")

	cfg := &Config{}
	EnableAlwaysOnLLM(cfg)

	if !cfg.Providers.Cloudflare.Enabled {
		t.Fatal("resolved Cloudflare credentials should enable the provider")
	}
}

func TestKombifyCloudConnected(t *testing.T) {
	if KombifyCloudConnected(nil) {
		t.Fatal("nil config is not cloud connected")
	}
	cfg := &Config{}
	cfg.ServerConnection.Enabled = true
	cfg.ServerConnection.URL = "https://speechkit.kombify.io"
	if !KombifyCloudConnected(cfg) {
		t.Fatal("hosted origin should count as kombify Cloud")
	}
	cfg.ServerConnection.URL = "http://127.0.0.1:8787"
	if KombifyCloudConnected(cfg) {
		t.Fatal("loopback should not count as kombify Cloud")
	}
}

func TestApplyAssemblyAILLMDefaultsFollowsVoiceAgentProvider(t *testing.T) {
	// A Voice Agent on AssemblyAI needs the LLM gateway for its session
	// summary even when the AssemblyAI speech provider toggle is off.
	cfg := &Config{}
	cfg.VoiceAgent.Provider = "assemblyai-agent"

	if !AssemblyAILLMGatewayWanted(cfg) {
		t.Fatal("voice agent on assemblyai should want the LLM gateway")
	}
	ApplyAssemblyAILLMDefaults(cfg)
	if cfg.Providers.AssemblyAI.LLMGatewayBaseURL != DefaultAssemblyAILLMGatewayBaseURL {
		t.Fatalf("gateway base URL = %q, want default", cfg.Providers.AssemblyAI.LLMGatewayBaseURL)
	}
	if cfg.Providers.AssemblyAI.LLMGatewayAgentModel != DefaultAssemblyAILLMGatewayAgentModel {
		t.Fatalf("agent model = %q, want default", cfg.Providers.AssemblyAI.LLMGatewayAgentModel)
	}
	if cfg.Providers.AssemblyAI.Enabled {
		t.Fatal("wanting the gateway must not flip the speech provider on")
	}

	other := &Config{}
	other.VoiceAgent.Provider = "deepgram"
	if AssemblyAILLMGatewayWanted(other) {
		t.Fatal("a Deepgram voice agent must not pull in the AssemblyAI gateway")
	}
}

// xAI is opt-in and never a default: enabling it for dictation, the Voice
// Agent or speech output must leave every Grok language-model slot empty, so
// no summary, Assist reply or cascaded-agent turn moves to Grok. Only picking
// a Grok Assist profile fills the slot.
func TestEnablingXAIFillsNoLanguageModelSlot(t *testing.T) {
	cfg := defaults()
	if err := SetProviderEnabled(cfg, "xai", true); err != nil {
		t.Fatalf("SetProviderEnabled: %v", err)
	}
	EnableAlwaysOnLLM(cfg)

	x := cfg.Providers.XAI
	if !x.Enabled {
		t.Fatal("xAI was not enabled")
	}
	if x.UtilityModel != "" || x.AssistModel != "" || x.AgentModel != "" {
		t.Fatalf("enabling xAI filled language models: utility=%q assist=%q agent=%q", x.UtilityModel, x.AssistModel, x.AgentModel)
	}
}

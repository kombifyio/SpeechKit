package config

// Microsoft Foundry endpoint derivation.
//
// Users paste the Foundry *project endpoint* from the portal
// (https://<account>.services.ai.azure.com/api/projects/<project>). Inference
// runs against the OpenAI-compatible v1 surface on the same host:
//
//	https://<host>/openai/v1/chat/completions
//	https://<host>/openai/v1/audio/transcriptions
//	https://<host>/openai/v1/audio/speech
//	wss://<host>/openai/v1/realtime?model=<deployment>
//
// The helpers here derive the per-layer base URLs from whatever endpoint
// shape the user pasted (project endpoint, account endpoint, or Azure OpenAI
// resource endpoint). Auth is API-key only (env AZURE_AI_API_KEY by default);
// the Foundry v1 surface accepts both `api-key: <KEY>` and
// `Authorization: Bearer <KEY>`.

import (
	"fmt"
	"net/url"
	"strings"
)

// Default Foundry deployment names per modality. Foundry's `model` request
// parameter is the *deployment name*; these defaults match the model-named
// deployments the portal suggests, and users can override each one.
const (
	DefaultFoundrySTTDeployment      = "gpt-4o-mini-transcribe"
	DefaultFoundryUtilityDeployment  = "gpt-5-mini"
	DefaultFoundryAssistDeployment   = "gpt-5.1"
	DefaultFoundryAgentDeployment    = "gpt-5.1"
	DefaultFoundryRealtimeDeployment = "gpt-realtime-2"
	DefaultFoundryTTSDeployment      = "gpt-4o-mini-tts"
	DefaultFoundryTTSVoice           = "alloy"
)

// FoundryInferenceHost extracts and validates the account host from a Foundry
// project endpoint, account endpoint, or Azure OpenAI resource endpoint.
// Returns e.g. "myaccount.services.ai.azure.com". HTTPS is enforced.
func FoundryInferenceHost(endpoint string) (string, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "", fmt.Errorf("foundry: project endpoint is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("foundry: invalid project endpoint: %w", err)
	}
	switch parsed.Scheme {
	case "https", "wss":
	default:
		return "", fmt.Errorf("foundry: project endpoint must use https (got %q)", parsed.Scheme)
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("foundry: project endpoint has no host")
	}
	return host, nil
}

// FoundryOpenAIBase returns the OpenAI-compatible base without the /v1
// segment, e.g. "https://<host>/openai". STT/openaicompat-style clients that
// append "v1/..." themselves use this base.
func FoundryOpenAIBase(endpoint string) (string, error) {
	host, err := FoundryInferenceHost(endpoint)
	if err != nil {
		return "", err
	}
	return "https://" + host + "/openai", nil
}

// FoundryInferenceBase returns the OpenAI-compatible v1 base, e.g.
// "https://<host>/openai/v1". LLM clients that append "chat/completions"
// use this base.
func FoundryInferenceBase(endpoint string) (string, error) {
	base, err := FoundryOpenAIBase(endpoint)
	if err != nil {
		return "", err
	}
	return base + "/v1", nil
}

// FoundryRealtimeURL returns the GA realtime WebSocket URL (without the model
// query parameter), e.g. "wss://<host>/openai/v1/realtime".
func FoundryRealtimeURL(endpoint string) (string, error) {
	host, err := FoundryInferenceHost(endpoint)
	if err != nil {
		return "", err
	}
	return "wss://" + host + "/openai/v1/realtime", nil
}

// ResolvedSTTDeployment returns the configured STT deployment or the default.
func (c FoundryProviderConfig) ResolvedSTTDeployment() string {
	return firstNonEmptyTrimmed(c.STTDeployment, DefaultFoundrySTTDeployment)
}

// ResolvedUtilityDeployment returns the utility-tier LLM deployment.
func (c FoundryProviderConfig) ResolvedUtilityDeployment() string {
	return firstNonEmptyTrimmed(c.UtilityDeployment, DefaultFoundryUtilityDeployment)
}

// ResolvedAssistDeployment returns the assist-tier LLM deployment.
func (c FoundryProviderConfig) ResolvedAssistDeployment() string {
	return firstNonEmptyTrimmed(c.AssistDeployment, DefaultFoundryAssistDeployment)
}

// ResolvedAgentDeployment returns the agent-tier LLM deployment.
func (c FoundryProviderConfig) ResolvedAgentDeployment() string {
	return firstNonEmptyTrimmed(c.AgentDeployment, DefaultFoundryAgentDeployment)
}

// ResolvedRealtimeDeployment returns the realtime (Voice Agent) deployment.
func (c FoundryProviderConfig) ResolvedRealtimeDeployment() string {
	return firstNonEmptyTrimmed(c.RealtimeDeployment, DefaultFoundryRealtimeDeployment)
}

// ResolvedTTSDeployment returns the TTS deployment.
func (c FoundryProviderConfig) ResolvedTTSDeployment() string {
	return firstNonEmptyTrimmed(c.TTSDeployment, DefaultFoundryTTSDeployment)
}

// ResolvedTTSVoice returns the TTS voice.
func (c FoundryProviderConfig) ResolvedTTSVoice() string {
	return firstNonEmptyTrimmed(c.TTSVoice, DefaultFoundryTTSVoice)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

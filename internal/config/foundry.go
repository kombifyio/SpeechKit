package config

// Microsoft Foundry endpoint derivation.
//
// Users paste the Foundry *project endpoint* from the portal
// (https://<account>.services.ai.azure.com/api/projects/<project>). One
// resource serves several surfaces, and every one of them is derived from
// that single value:
//
//	https://<host>/openai/v1/chat/completions          OpenAI-publisher deployments
//	https://<host>/openai/v1/audio/transcriptions      gpt-4o-*-transcribe deployments
//	https://<host>/openai/v1/audio/speech              gpt-4o-mini-tts deployments
//	https://<host>/mai/v1/chat/completions             Microsoft-publisher deployments (MAI-Thinking-1)
//	wss://<host>/openai/v1/realtime?model=<deployment> OpenAI Realtime protocol
//	wss://<host>/voice-live/realtime                   Voice Live (managed voice agent)
//	https://<host>/api/projects/<project>/deployments  deployment discovery
//	https://<resource>.cognitiveservices.azure.com/    Azure Speech: MAI-Voice-2 TTS and
//	                                                   MAI-Transcribe-2 fast transcription
//
// Auth is either the resource key (api-key / Ocp-Apim-Subscription-Key) or a
// bearer token minted for the signed-in Microsoft identity; see AuthMode.

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

// MAI speech defaults. These are not deployments: the Speech surface of the
// resource serves them directly, so the names are model ids and voice short
// names exactly as Azure Speech spells them.
const (
	FoundryMAITranscribeModel            = "MAI-Transcribe-2"
	FoundryMAIVoiceModel                 = "MAI-Voice-2"
	FoundryMAIVoiceFlashModel            = "MAI-Voice-2-Flash"
	DefaultFoundryMAIVoice               = "en-US-Harper:MAI-Voice-2"
	DefaultFoundrySTTStyle               = "clean"
	DefaultFoundryVoiceLiveModel         = "gpt-realtime-2"
	DefaultFoundryVoiceLiveVoice         = "en-US-Harper:MAI-Voice-2-Flash"
	DefaultFoundryVoiceLiveTranscription = "mai-transcribe-2"
	DefaultFoundryMAIThinkingModel       = "MAI-Thinking-1"
)

// Auth mode and credential source values for [providers.foundry].
const (
	FoundryAuthModeAPIKey = "api_key"
	FoundryAuthModeEntra  = "entra"

	FoundryEntraCredentialAuto       = "auto"
	FoundryEntraCredentialAzureCLI   = "azure_cli"
	FoundryEntraCredentialBrowser    = "browser"
	FoundryEntraCredentialDeviceCode = "device_code"

	FoundryAzureCLIProfileShared   = "shared"
	FoundryAzureCLIProfileIsolated = "isolated"
)

// Engine values returned by STTEngine / TTSEngine.
const (
	FoundryEngineOpenAI = "openai"
	FoundryEngineSpeech = "speech"
)

// Entra scopes. The project API (deployment discovery) only accepts tokens
// for the Foundry audience; inference, Speech and Voice Live accept the
// Cognitive Services audience. Both come from one sign-in.
const (
	FoundryScopeAI                = "https://ai.azure.com/.default"
	FoundryScopeCognitiveServices = "https://cognitiveservices.azure.com/.default"
)

// FoundryProjectDeploymentsAPIVersion is the data-plane version of
// GET {projectEndpoint}/deployments.
const FoundryProjectDeploymentsAPIVersion = "v1"

// FoundryInferenceHost extracts and validates the account host from a Foundry
// project endpoint, account endpoint, or Azure OpenAI resource endpoint.
// Returns e.g. "myaccount.services.ai.azure.com". HTTPS is enforced.
func FoundryInferenceHost(endpoint string) (string, error) {
	parsed, err := parseFoundryEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.Host), nil
}

func parseFoundryEndpoint(endpoint string) (*url.URL, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return nil, fmt.Errorf("foundry: project endpoint is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("foundry: invalid project endpoint: %w", err)
	}
	switch parsed.Scheme {
	case "https", "wss":
	default:
		return nil, fmt.Errorf("foundry: project endpoint must use https (got %q)", parsed.Scheme)
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("foundry: project endpoint has no host")
	}
	return parsed, nil
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

// FoundryMAIInferenceBase returns the base for Microsoft-publisher
// deployments such as MAI-Thinking-1, e.g. "https://<host>/mai/v1". Those
// models are not served on /openai/v1; the request shape is still OpenAI
// chat completions, with max_completion_tokens instead of max_tokens.
func FoundryMAIInferenceBase(endpoint string) (string, error) {
	host, err := FoundryInferenceHost(endpoint)
	if err != nil {
		return "", err
	}
	return "https://" + host + "/mai/v1", nil
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

// FoundryVoiceLiveURL returns the Voice Live WebSocket base (without the
// api-version and model query parameters), e.g.
// "wss://<host>/voice-live/realtime".
func FoundryVoiceLiveURL(endpoint string) (string, error) {
	host, err := FoundryInferenceHost(endpoint)
	if err != nil {
		return "", err
	}
	return "wss://" + host + "/voice-live/realtime", nil
}

// FoundryProjectDeploymentsURL returns the data-plane deployment listing for
// the project, e.g.
// "https://<host>/api/projects/<project>/deployments?api-version=v1". It
// requires a real project endpoint: an account or Azure OpenAI resource
// endpoint carries no project segment, and there is nothing to list without
// one.
func FoundryProjectDeploymentsURL(endpoint string) (string, error) {
	parsed, err := parseFoundryEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	path := strings.Trim(parsed.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) < 3 || segments[0] != "api" || segments[1] != "projects" || strings.TrimSpace(segments[2]) == "" {
		return "", fmt.Errorf("foundry: %q is not a project endpoint (expected https://<host>/api/projects/<project>)", strings.TrimSpace(endpoint))
	}
	return fmt.Sprintf("https://%s/api/projects/%s/deployments?api-version=%s",
		strings.TrimSpace(parsed.Host), url.PathEscape(segments[2]), FoundryProjectDeploymentsAPIVersion), nil
}

// FoundrySpeechHost derives the Azure Speech host of the same resource, e.g.
// "myaccount.cognitiveservices.azure.com". Speech does not listen on the
// services.ai.azure.com or openai.azure.com aliases; the Cognitive Services
// custom domain is the one host that accepts both the resource key and an
// Entra bearer token, which is why no region is needed. Hosts outside the
// known public suffixes (private link, sovereign clouds) are returned
// unchanged so an operator can still point at their own Speech host.
func FoundrySpeechHost(endpoint string) (string, error) {
	host, err := FoundryInferenceHost(endpoint)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(host)
	for _, suffix := range []string{".services.ai.azure.com", ".openai.azure.com", ".cognitiveservices.azure.com"} {
		if strings.HasSuffix(lower, suffix) {
			resource := host[:len(host)-len(suffix)]
			if strings.TrimSpace(resource) == "" {
				return "", fmt.Errorf("foundry: endpoint host %q has no resource name", host)
			}
			return resource + ".cognitiveservices.azure.com", nil
		}
	}
	return host, nil
}

// IsMAITranscribeModel reports whether an STT deployment name addresses the
// MAI-Transcribe family served by Azure Speech fast transcription.
func IsMAITranscribeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "mai-transcribe")
}

// IsMAIVoiceModel reports whether a TTS deployment or voice name addresses the
// MAI-Voice family served by Azure Speech (either the family name such as
// "MAI-Voice-2" or a voice short name such as "de-DE-Mia:MAI-Voice-2").
func IsMAIVoiceModel(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(lower, "mai-voice") || strings.Contains(lower, ":mai-voice")
}

// IsMAIThinkingModel reports whether an LLM deployment name addresses a
// Microsoft-publisher model served on /mai/v1.
func IsMAIThinkingModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "mai-thinking")
}

// ResolvedAuthMode returns "api_key" or "entra".
func (c FoundryProviderConfig) ResolvedAuthMode() string {
	if strings.EqualFold(strings.TrimSpace(c.AuthMode), FoundryAuthModeEntra) {
		return FoundryAuthModeEntra
	}
	return FoundryAuthModeAPIKey
}

// UsesEntra reports whether the adapters should send bearer tokens from the
// signed-in identity instead of the resource key.
func (c FoundryProviderConfig) UsesEntra() bool {
	return c.ResolvedAuthMode() == FoundryAuthModeEntra
}

// ResolvedEntraCredential normalizes the credential source; unknown values
// fall back to "auto".
func (c FoundryProviderConfig) ResolvedEntraCredential() string {
	switch strings.ToLower(strings.TrimSpace(c.EntraCredential)) {
	case FoundryEntraCredentialAzureCLI, "azurecli", "az", "cli":
		return FoundryEntraCredentialAzureCLI
	case FoundryEntraCredentialBrowser, "interactive":
		return FoundryEntraCredentialBrowser
	case FoundryEntraCredentialDeviceCode, "devicecode", "device-code":
		return FoundryEntraCredentialDeviceCode
	default:
		return FoundryEntraCredentialAuto
	}
}

// ResolvedAzureCLIProfile returns "shared" or "isolated".
func (c FoundryProviderConfig) ResolvedAzureCLIProfile() string {
	if strings.EqualFold(strings.TrimSpace(c.AzureCLIProfile), FoundryAzureCLIProfileIsolated) {
		return FoundryAzureCLIProfileIsolated
	}
	return FoundryAzureCLIProfileShared
}

// ResolvedSTTDeployment returns the configured STT deployment or the default.
func (c FoundryProviderConfig) ResolvedSTTDeployment() string {
	return firstNonEmptyTrimmed(c.STTDeployment, DefaultFoundrySTTDeployment)
}

// STTEngine reports which surface serves dictation: "speech" for
// MAI-Transcribe models, "openai" for gpt-4o-*-transcribe deployments.
func (c FoundryProviderConfig) STTEngine() string {
	if IsMAITranscribeModel(c.ResolvedSTTDeployment()) {
		return FoundryEngineSpeech
	}
	return FoundryEngineOpenAI
}

// ResolvedSTTStyle returns "clean" or "verbatim" for the MAI-Transcribe path.
func (c FoundryProviderConfig) ResolvedSTTStyle() string {
	if strings.EqualFold(strings.TrimSpace(c.STTStyle), "verbatim") {
		return "verbatim"
	}
	return DefaultFoundrySTTStyle
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

// TTSEngine reports which surface serves speech output: "speech" for
// MAI-Voice models, "openai" for gpt-4o-mini-tts style deployments.
func (c FoundryProviderConfig) TTSEngine() string {
	if IsMAIVoiceModel(c.ResolvedTTSDeployment()) || IsMAIVoiceModel(c.TTSVoice) {
		return FoundryEngineSpeech
	}
	return FoundryEngineOpenAI
}

// ResolvedTTSVoice returns the TTS voice. On the Speech engine the voice is a
// Speech short name; an OpenAI voice name left over from a previous
// deployment choice is replaced by the MAI default so the request does not
// fail on a voice the Speech service has never heard of.
func (c FoundryProviderConfig) ResolvedTTSVoice() string {
	voice := strings.TrimSpace(c.TTSVoice)
	if c.TTSEngine() == FoundryEngineSpeech {
		if voice == "" || !strings.Contains(voice, "-") {
			return defaultMAIVoiceForFamily(c.ResolvedTTSDeployment())
		}
		return voice
	}
	return firstNonEmptyTrimmed(voice, DefaultFoundryTTSVoice)
}

// defaultMAIVoiceForFamily picks the default voice of the requested MAI
// family so that "MAI-Voice-2-Flash" as the deployment name yields a Flash
// voice, not the higher-latency HD one.
func defaultMAIVoiceForFamily(family string) string {
	if strings.EqualFold(strings.TrimSpace(family), FoundryMAIVoiceFlashModel) {
		return "en-US-Harper:MAI-Voice-2-Flash"
	}
	return DefaultFoundryMAIVoice
}

// ResolvedVoiceLiveModel returns the Voice Live brain model.
func (c FoundryProviderConfig) ResolvedVoiceLiveModel() string {
	return firstNonEmptyTrimmed(c.VoiceLiveModel, DefaultFoundryVoiceLiveModel)
}

// ResolvedVoiceLiveVoice returns the Voice Live output voice.
func (c FoundryProviderConfig) ResolvedVoiceLiveVoice() string {
	return firstNonEmptyTrimmed(c.VoiceLiveVoice, DefaultFoundryVoiceLiveVoice)
}

// ResolvedVoiceLiveTranscription returns the Voice Live input transcription
// model (lower-case, as the session API spells it).
func (c FoundryProviderConfig) ResolvedVoiceLiveTranscription() string {
	return strings.ToLower(firstNonEmptyTrimmed(c.VoiceLiveTranscription, DefaultFoundryVoiceLiveTranscription))
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

package models

import (
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// STTRouterSpec describes an STT router in plain values: the routing knobs
// and, per provider, the options a host resolved from its configuration. A
// nil provider field is not built. Like STTBuildSpec it names no provider
// package, so a host can describe the router it wants without linking the
// providers; which ones can be constructed is the router builder's concern.
//
// The adapter ids (Adapters, Drop) are the provider-policy ids:
// "local", "huggingface", "openrouter", "vps", "ollama", "groq", "openai",
// "deepgram", "assemblyai", "google", "foundry" (Foundry and
// FoundrySpeech) and "xai".
type STTRouterSpec struct {
	Strategy             stt.Strategy
	PreferLocalUnderSecs float64
	ParallelCloud        bool
	ReplaceOnBetter      bool
	// OnProviderSelected is installed on the router (audit logging).
	OnProviderSelected stt.ProviderSelectedObserver
	// Secrets resolves credentials providers read lazily (Google
	// streaming). Nil falls back to the process environment.
	Secrets stt.SecretResolver

	Local         *LocalSTTSpec
	HuggingFace   *KeyedSTTSpec
	OpenRouter    *KeyedSTTSpec
	VPS           *EndpointSTTSpec
	Ollama        *EndpointSTTSpec
	Groq          *KeyedSTTSpec
	OpenAI        *KeyedSTTSpec
	Deepgram      *DeepgramSTTSpec
	AssemblyAI    *AssemblyAISTTSpec
	Google        *GoogleSTTSpec
	Foundry       *FoundrySTTSpec
	FoundrySpeech *FoundrySpeechOpts
	// XAI is the opt-in xAI Grok Voice Transcribe provider.
	XAI *XAISTTSpec
}

// LocalSTTSpec is the host-managed whisper.cpp provider (registered, not
// started).
type LocalSTTSpec struct {
	Port      int
	ModelPath string
	GPU       string
}

// KeyedSTTSpec is a cloud provider reached with an API key (or token) and a
// model id.
type KeyedSTTSpec struct {
	APIKey string
	Model  string
}

// XAISTTSpec is the opt-in xAI Grok Voice Transcribe provider. Selected is
// set when model_selection.dictate names an xAI profile; an unselected xAI
// provider is registered but explicit-only: the router uses it only for a
// request that names xAI and never as a fallback for other providers.
type XAISTTSpec struct {
	APIKey   string
	Model    string
	Selected bool
}

// EndpointSTTSpec is a provider on a host the user operates (a self-hosted
// whisper-server, Ollama). Validation, when set, replaces the provider's
// default endpoint validation (a restricted network scope requires a local
// address).
type EndpointSTTSpec struct {
	URL        string
	APIKey     string
	Model      string
	Validation *netsec.ValidationOptions
}

// DeepgramSTTSpec is the Deepgram Listen provider.
type DeepgramSTTSpec struct {
	APIKey           string
	Model            string
	DiarizationModel string
	Listen           DeepgramTuning
}

// AssemblyAISTTSpec is the AssemblyAI provider; empty fields keep the
// provider defaults.
type AssemblyAISTTSpec struct {
	APIKey            string
	Models            string
	StreamingModel    string
	StreamingBaseURL  string
	SyncBaseURL       string
	DisableSync       bool
	StreamingLLM      bool
	StreamingLLMModel string
}

// GoogleSTTSpec is the opt-in Google Cloud Speech-to-Text provider (or the
// Gemini Transcribe adapter for a Gemini model id).
type GoogleSTTSpec struct {
	APIKey                    string
	Model                     string
	CredentialsJSONEnv        string
	ApplicationCredentialsEnv string
	Region                    string
}

// FoundrySTTSpec is the OpenAI-compatible transcription route of a
// Microsoft Foundry project.
type FoundrySTTSpec struct {
	APIKey      string
	BearerToken framework.BearerTokenFunc
	BaseURL     string
	Model       string
}

// Adapters lists the policy adapter ids of the providers s carries, in the
// router's assembly order. A nil spec carries none.
func (s *STTRouterSpec) Adapters() []string {
	if s == nil {
		return nil
	}
	var ids []string
	add := func(id string, present bool) {
		if present {
			ids = append(ids, id)
		}
	}
	add("local", s.Local != nil)
	add("huggingface", s.HuggingFace != nil)
	add("openrouter", s.OpenRouter != nil)
	add("vps", s.VPS != nil)
	add("ollama", s.Ollama != nil)
	add("groq", s.Groq != nil)
	add("openai", s.OpenAI != nil)
	add("deepgram", s.Deepgram != nil)
	add("assemblyai", s.AssemblyAI != nil)
	add("google", s.Google != nil)
	add("foundry", s.Foundry != nil || s.FoundrySpeech != nil)
	add("xai", s.XAI != nil)
	return ids
}

// Drop removes the provider of adapter from s; an unknown id changes
// nothing.
func (s *STTRouterSpec) Drop(adapter string) {
	if s == nil {
		return
	}
	switch adapter {
	case "local":
		s.Local = nil
	case "huggingface":
		s.HuggingFace = nil
	case "openrouter":
		s.OpenRouter = nil
	case "vps":
		s.VPS = nil
	case "ollama":
		s.Ollama = nil
	case "groq":
		s.Groq = nil
	case "openai":
		s.OpenAI = nil
	case "deepgram":
		s.Deepgram = nil
	case "assemblyai":
		s.AssemblyAI = nil
	case "google":
		s.Google = nil
	case "foundry":
		s.Foundry = nil
		s.FoundrySpeech = nil
	case "xai":
		s.XAI = nil
	}
}

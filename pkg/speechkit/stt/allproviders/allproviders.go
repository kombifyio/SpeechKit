// Package allproviders is the batteries-included STT assembly layer: it knows
// every provider SpeechKit ships and turns a host's resolved credentials into
// a ready [stt.Router].
//
// Importing it compiles every provider, which is the right trade for a host
// that offers a provider choice at runtime — the desktop app, the server. A
// host that speaks to exactly one backend imports that provider's own package
// and assembles the router itself, and stays free of the others'
// dependencies.
//
// It is the single source of truth for STT provider assembly:
//
//   - Build/Register map a provider id (or ExecutionMode) to the canonical
//     provider name, endpoint, and constructor.
//   - BuildRouter assembles a [stt.Router] from a set of enabled providers so
//     the Device- and Server-Targets share one assembly path while each keeps
//     its own config-resolution specifics (mirrors pkg/speechkit/tts.BuildRouter).
package allproviders

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/google"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/huggingface"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openrouter"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/vps"
)

// BuildSpec carries the inputs needed to construct a cloud STT provider for a
// given ExecutionMode. The host config layer resolves secrets and passes them
// in; the registry owns the provider's canonical name, endpoint, and
// constructor.
type BuildSpec struct {
	ExecutionMode speechkit.ExecutionMode
	Provider      string
	ModelID       string

	APIKey  string // cloud API key (OpenAI/Groq/Google/Deepgram/AssemblyAI/OpenRouter/Foundry)
	Token   string // HuggingFace token
	BaseURL string // Ollama base URL (optional; defaulted when empty) or Foundry OpenAI-compatible base (required)

	// DiarizationModel overrides the Deepgram diarization model (optional).
	DiarizationModel string
	// Deepgram forwards provider-specific Listen options (optional).
	Deepgram deepgram.Options
	// Google streaming credential env-var names (optional), forwarded to the
	// Google provider so realtime transcription can authenticate.
	GoogleStreamingCredentialsEnv   string
	GoogleApplicationCredentialsEnv string
}

// Build constructs the cloud STT provider for spec and returns its canonical
// Name plus the provider. spec.Provider (a provider id or profile id) wins;
// when empty, the provider id is derived from spec.ExecutionMode.
//
// ExecutionModeLocal is host-managed (whisper.cpp subprocess lifecycle) and is
// intentionally not handled here.
func Build(spec BuildSpec) (string, stt.STTProvider, error) {
	providerID := catalog.NormalizeProviderID(spec.Provider)
	if providerID == "" {
		providerID = catalog.ProviderIDForExecutionMode(spec.ExecutionMode)
	}
	registryMu.RLock()
	descriptor, ok := providerRegistry[providerID]
	registryMu.RUnlock()
	if !ok {
		return "", nil, fmt.Errorf("stt: unsupported provider %q for execution mode %q", providerID, spec.ExecutionMode)
	}
	provider, err := descriptor.Build(spec)
	if err != nil {
		return "", nil, err
	}
	return descriptor.Name, provider, nil
}

// Register adds a provider constructor under the given id so hosts can extend
// the Build mapping with custom providers. The id is normalized like
// spec.Provider in Build. Registering an id that already exists (including
// the built-ins) returns an error.
func Register(id, name string, build func(BuildSpec) (stt.STTProvider, error)) error {
	providerID := catalog.NormalizeProviderID(id)
	if providerID == "" {
		return fmt.Errorf("stt: provider id is required")
	}
	if build == nil {
		return fmt.Errorf("stt: provider %q build func is required", providerID)
	}
	if strings.TrimSpace(name) == "" {
		name = providerID
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := providerRegistry[providerID]; exists {
		return fmt.Errorf("stt: provider %q is already registered", providerID)
	}
	providerRegistry[providerID] = providerDescriptor{Name: name, Build: build}
	return nil
}

type providerDescriptor struct {
	Name  string
	Build func(BuildSpec) (stt.STTProvider, error)
}

var registryMu sync.RWMutex

var providerRegistry = map[string]providerDescriptor{
	"huggingface": {
		Name: "huggingface",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			return huggingface.New(spec.ModelID, spec.Token), nil
		},
	},
	"openai": {
		Name: "openai",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			return openaicompat.New("openai", "https://api.openai.com", spec.APIKey, spec.ModelID), nil
		},
	},
	"groq": {
		Name: "groq",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			return openaicompat.New("groq", "https://api.groq.com/openai", spec.APIKey, spec.ModelID), nil
		},
	},
	"google": {
		Name: "google",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			provider := google.New(spec.APIKey, spec.ModelID)
			provider.SetStreamingCredentialEnvs(spec.GoogleStreamingCredentialsEnv, spec.GoogleApplicationCredentialsEnv)
			return provider, nil
		},
	},
	"deepgram": {
		Name: "deepgram",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			provider := deepgram.New(spec.APIKey, spec.ModelID)
			if spec.DiarizationModel != "" {
				provider.DiarizationModel = spec.DiarizationModel
			}
			if hasDeepgramOptions(spec.Deepgram) {
				provider.ApplyOptions(spec.Deepgram)
			}
			return provider, nil
		},
	},
	"assemblyai": {
		Name: "assemblyai",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			return assemblyai.New(spec.APIKey, spec.ModelID), nil
		},
	},
	"openrouter": {
		Name: "openrouter",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			return openrouter.New(spec.APIKey, spec.ModelID), nil
		},
	},
	"foundry": {
		Name: "foundry",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			baseURL := strings.TrimSpace(spec.BaseURL)
			if baseURL == "" {
				return nil, fmt.Errorf("stt: foundry requires the OpenAI-compatible base URL derived from the project endpoint")
			}
			model := strings.TrimSpace(spec.ModelID)
			if model == "" {
				model = "gpt-4o-mini-transcribe"
			}
			return openaicompat.New("foundry", baseURL, spec.APIKey, model), nil
		},
	},
	"ollama": {
		Name: "ollama",
		Build: func(spec BuildSpec) (stt.STTProvider, error) {
			baseURL := spec.BaseURL
			if baseURL == "" {
				baseURL = "http://localhost:11434"
			}
			return openaicompat.NewOllama(baseURL, spec.ModelID), nil
		},
	},
}

func hasDeepgramOptions(opts deepgram.Options) bool {
	return opts.Configured ||
		opts.SmartFormat ||
		opts.Dictation ||
		opts.FillerWords ||
		opts.Numerals ||
		opts.DetectLanguage ||
		opts.UseVocabularyKeyterms ||
		opts.LanguageOverride != "" ||
		len(opts.Keyterms) > 0 ||
		opts.EndpointingMs != 0
}

// RouterConfig carries the routing knobs both hosts resolve from their own
// config before delegating router assembly to BuildRouter.
type RouterConfig struct {
	Strategy             stt.Strategy
	PreferLocalUnderSecs float64
	ParallelCloud        bool
	ReplaceOnBetter      bool
	// PreferredProfileID optionally pins the cloud provider matching this
	// model_selection profile (or bare provider name) to the front of the
	// strategy order, analogous to tts.EnabledProviders.PreferredProfileID.
	PreferredProfileID string
	// OnProviderSelected is installed as the router's per-instance observer
	// (stt.Router.OnProviderSelected). Hosts wire audit logging here.
	OnProviderSelected stt.ProviderSelectedObserver
}

// Per-provider assembly options. These carry the union of what the Device-
// and Server-Targets configure; nil fields in EnabledProviders are skipped.
// (deepgram.Options is the existing Listen-option type; the assembly struct is
// DeepgramOpts and embeds it as Listen.)
type (
	// LocalOpts configures the host-managed whisper.cpp provider. The
	// provider is registered but not started; process lifecycle stays with
	// the host.
	LocalOpts struct {
		Port      int
		ModelPath string
		GPU       string
	}

	// VPSOpts configures a self-hosted OpenAI-compatible whisper-server.
	// Model defaults to "whisper-1" when empty.
	VPSOpts struct {
		URL    string
		APIKey string
		Model  string
		// Validation, when non-nil, replaces the provider's default netsec
		// validation (loopback+private+http). Restricted network scopes pass
		// a RequireLocal option set so public endpoints are rejected at
		// request and dial time.
		Validation *netsec.ValidationOptions
	}

	HuggingFaceOpts struct {
		Model string
		Token string
	}

	// OpenAIOpts: Model defaults to "whisper-1" when empty.
	OpenAIOpts struct {
		APIKey string
		Model  string
	}

	// GroqOpts: Model defaults to "whisper-large-v3-turbo" when empty.
	GroqOpts struct {
		APIKey string
		Model  string
	}

	GoogleOpts struct {
		APIKey string
		Model  string
		// Streaming credential env-var names forwarded via
		// SetStreamingCredentialEnvs.
		CredentialsJSONEnv        string
		ApplicationCredentialsEnv string
	}

	DeepgramOpts struct {
		APIKey string
		Model  string
		// DiarizationModel overrides the provider default when non-empty.
		DiarizationModel string
		// Listen forwards Deepgram Listen options (applied when configured).
		Listen deepgram.Options
	}

	AssemblyAIOpts struct {
		APIKey string
		// Models is the comma-separated STT model list accepted by
		// assemblyai.New.
		Models           string
		StreamingModel   string
		StreamingBaseURL string
		SyncBaseURL      string
		DisableSync      bool
		// StreamingLLM enables LLM Gateway cleanup on realtime dictation with
		// StreamingLLMModel.
		StreamingLLM      bool
		StreamingLLMModel string
	}

	OpenRouterOpts struct {
		APIKey string
		Model  string
	}

	// FoundryOpts configures the Microsoft Foundry provider. BaseURL is the
	// OpenAI-compatible base derived from the project endpoint
	// (https://<host>/openai); Model is the deployment name and defaults to
	// "gpt-4o-mini-transcribe" when empty.
	FoundryOpts struct {
		APIKey  string
		BaseURL string
		Model   string
	}

	// OllamaOpts: BaseURL defaults to "http://localhost:11434" and Model to
	// the provider default when empty.
	OllamaOpts struct {
		BaseURL string
		Model   string
		// Validation, when non-nil, replaces the provider's default netsec
		// validation (loopback+private+http). Restricted network scopes pass
		// a RequireLocal option set so public endpoints are rejected at
		// request and dial time.
		Validation *netsec.ValidationOptions
	}
)

// EnabledProviders carries the per-provider options a host has already
// resolved from its own config (credential stores, env secrets, model
// defaults). Nil fields are skipped. Extra providers are appended after the
// named ones as additional cloud candidates.
type EnabledProviders struct {
	Local       *LocalOpts
	HuggingFace *HuggingFaceOpts
	OpenRouter  *OpenRouterOpts
	VPS         *VPSOpts
	Ollama      *OllamaOpts
	Groq        *GroqOpts
	OpenAI      *OpenAIOpts
	Deepgram    *DeepgramOpts
	AssemblyAI  *AssemblyAIOpts
	Google      *GoogleOpts
	Foundry     *FoundryOpts
	Extra       []stt.STTProvider
	// Secrets is handed to every constructed provider that resolves
	// credentials lazily (currently Google streaming). Nil falls back to the
	// process environment.
	Secrets stt.SecretResolver
}

// BuildRouter is the single source of truth for assembling an STT router from
// a set of enabled providers: it constructs each enabled provider in a stable
// order (cloud fallback order: HuggingFace, OpenRouter, VPS, Ollama, Groq,
// OpenAI, Deepgram, AssemblyAI, Google, Foundry, then Extra), applies optional
// model_selection pinning, and returns the router plus human-readable notes.
// ok is false (router nil) when nothing is enabled.
func BuildRouter(cfg RouterConfig, enabled EnabledProviders) (router *stt.Router, ok bool, notes []string) {
	var cloud []stt.STTProvider

	if o := enabled.HuggingFace; o != nil {
		cloud = append(cloud, huggingface.New(o.Model, o.Token))
		notes = append(notes, "STT: HuggingFace registered (model="+o.Model+")")
	}
	if o := enabled.OpenRouter; o != nil {
		cloud = append(cloud, openrouter.New(o.APIKey, o.Model))
		notes = append(notes, "STT: OpenRouter registered (model="+o.Model+")")
	}
	if o := enabled.VPS; o != nil {
		p := vps.NewWithModel(o.URL, o.APIKey, o.Model)
		if o.Validation != nil {
			// The provider's HTTP client dials through &p.Validation, so
			// tightening the field also tightens dial-time IP checks.
			p.Validation = *o.Validation
		}
		cloud = append(cloud, p)
		notes = append(notes, "STT: VPS registered (url="+o.URL+", model="+p.Model+")")
	}
	if o := enabled.Ollama; o != nil {
		p := openaicompat.NewOllama(o.BaseURL, o.Model)
		if o.Validation != nil {
			p.Validation = *o.Validation
		}
		cloud = append(cloud, p)
		notes = append(notes, "STT: Ollama registered (model="+p.Model+")")
	}
	if o := enabled.Groq; o != nil {
		model := strings.TrimSpace(o.Model)
		if model == "" {
			model = "whisper-large-v3-turbo"
		}
		cloud = append(cloud, openaicompat.New("groq", "https://api.groq.com/openai", o.APIKey, model))
		notes = append(notes, "STT: Groq registered (model="+model+")")
	}
	if o := enabled.OpenAI; o != nil {
		model := strings.TrimSpace(o.Model)
		if model == "" {
			model = "whisper-1"
		}
		cloud = append(cloud, openaicompat.New("openai", "https://api.openai.com", o.APIKey, model))
		notes = append(notes, "STT: OpenAI registered (model="+model+")")
	}
	if o := enabled.Deepgram; o != nil {
		p := deepgram.New(o.APIKey, o.Model)
		if strings.TrimSpace(o.DiarizationModel) != "" {
			p.DiarizationModel = o.DiarizationModel
		}
		if hasDeepgramOptions(o.Listen) {
			p.ApplyOptions(o.Listen)
		}
		cloud = append(cloud, p)
		notes = append(notes, "STT: Deepgram registered (model="+p.Model+")")
	}
	if o := enabled.AssemblyAI; o != nil {
		p := assemblyai.New(o.APIKey, o.Models)
		if strings.TrimSpace(o.StreamingModel) != "" {
			p.StreamingModel = o.StreamingModel
		}
		if strings.TrimSpace(o.StreamingBaseURL) != "" {
			p.StreamingBaseURL = o.StreamingBaseURL
		}
		if strings.TrimSpace(o.SyncBaseURL) != "" {
			p.SyncBaseURL = o.SyncBaseURL
		}
		p.DisableSync = o.DisableSync
		if o.StreamingLLM {
			p.EnableStreamingLLM(o.StreamingLLMModel, "", 256)
		}
		cloud = append(cloud, p)
		notes = append(notes, "STT: AssemblyAI registered (models="+strings.Join(p.Models, ",")+")")
	}
	if o := enabled.Google; o != nil {
		p := google.New(o.APIKey, o.Model)
		p.SetStreamingCredentialEnvs(o.CredentialsJSONEnv, o.ApplicationCredentialsEnv)
		p.SecretResolver = enabled.Secrets
		cloud = append(cloud, p)
		notes = append(notes, "STT: Google registered (model="+o.Model+")")
	}
	if o := enabled.Foundry; o != nil {
		if baseURL := strings.TrimSpace(o.BaseURL); baseURL != "" {
			model := strings.TrimSpace(o.Model)
			if model == "" {
				model = "gpt-4o-mini-transcribe"
			}
			cloud = append(cloud, openaicompat.New("foundry", baseURL, o.APIKey, model))
			notes = append(notes, "STT: Microsoft Foundry registered (deployment="+model+")")
		} else {
			notes = append(notes, "STT: Microsoft Foundry skipped (project endpoint missing)")
		}
	}
	for _, p := range enabled.Extra {
		if p == nil {
			continue
		}
		cloud = append(cloud, p)
		notes = append(notes, "STT: extra provider registered ("+p.Name()+")")
	}

	var localProvider stt.STTProvider
	if o := enabled.Local; o != nil {
		localProvider = local.New(o.Port, o.ModelPath, o.GPU)
		notes = append(notes, "STT: local whisper.cpp registered (not started)")
	}

	if localProvider == nil && len(cloud) == 0 {
		return nil, false, notes
	}

	if profileID := strings.TrimSpace(cfg.PreferredProfileID); profileID != "" {
		if preferred := stt.ProviderNameFromProfileID(profileID); preferred != "" {
			ordered := stt.PrioritizeProviderProfile(cloud, profileID)
			if len(ordered) > 0 && strings.EqualFold(ordered[0].Name(), preferred) {
				notes = append(notes, "STT: model_selection profile "+profileID+" pinned provider "+preferred+" first")
			} else {
				notes = append(notes, "STT: model_selection profile "+profileID+" requested provider "+preferred+" but it is not configured; using strategy order")
			}
			cloud = ordered
		}
	}

	router = &stt.Router{
		Strategy:             cfg.Strategy,
		PreferLocalUnderSecs: cfg.PreferLocalUnderSecs,
		ParallelCloud:        cfg.ParallelCloud,
		ReplaceOnBetter:      cfg.ReplaceOnBetter,
		OnProviderSelected:   cfg.OnProviderSelected,
	}
	if localProvider != nil {
		router.SetLocal(localProvider)
	}
	router.SetCloudProviders(cloud)
	return router, true, notes
}

package stt

// This file re-exports the STT adapters that now live in the public
// pkg/speechkit/stt package, so the existing call sites in cmd/speechkit,
// internal/server, internal/router, internal/serverclient, tools, etc. keep
// compiling unchanged. The Build factory (registry.go) keeps its host-facing
// BuildSpec typed with the internal models.ExecutionMode and forwards to the
// public registry in pkg/speechkit/stt. New adapter and assembly code goes in
// pkg/speechkit/stt.

import pkgstt "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Router assembly (single public assembly path shared by the Device- and
// Server-Targets; see pkg/speechkit/stt.BuildRouter).
type (
	RouterConfig     = pkgstt.RouterConfig
	EnabledProviders = pkgstt.EnabledProviders
	LocalOpts        = pkgstt.LocalOpts
	VPSOpts          = pkgstt.VPSOpts
	HuggingFaceOpts  = pkgstt.HuggingFaceOpts
	OpenAIOpts       = pkgstt.OpenAIOpts
	GroqOpts         = pkgstt.GroqOpts
	GoogleOpts       = pkgstt.GoogleOpts
	DeepgramOpts     = pkgstt.DeepgramOpts
	AssemblyAIOpts   = pkgstt.AssemblyAIOpts
	OpenRouterOpts   = pkgstt.OpenRouterOpts
	OllamaOpts       = pkgstt.OllamaOpts
)

// BuildRouter forwards to the public router assembly SSOT.
var BuildRouter = pkgstt.BuildRouter

// Core contracts.
type (
	STTProvider               = pkgstt.STTProvider
	TranscribeOpts            = pkgstt.TranscribeOpts
	Result                    = pkgstt.Result
	WordConfidence            = pkgstt.WordConfidence
	ResolvedTranscribeOptions = pkgstt.ResolvedTranscribeOptions
	CapabilityReporter        = pkgstt.CapabilityReporter
	InstallStatus             = pkgstt.InstallStatus
)

// Concrete provider types.
type (
	GoogleSTTProvider        = pkgstt.GoogleSTTProvider
	DeepgramProvider         = pkgstt.DeepgramProvider
	DeepgramOptions          = pkgstt.DeepgramOptions
	AssemblyAIProvider       = pkgstt.AssemblyAIProvider
	HuggingFaceProvider      = pkgstt.HuggingFaceProvider
	LocalProvider            = pkgstt.LocalProvider
	OpenAICompatibleProvider = pkgstt.OpenAICompatibleProvider
	OpenRouterSTTProvider    = pkgstt.OpenRouterSTTProvider
	VPSProvider              = pkgstt.VPSProvider
)

const MinWhisperModelBytes = pkgstt.MinWhisperModelBytes

// LanguageMulti is the value carried when no language is pinned. Never
// forwarded verbatim — each provider expresses multilanguage in its own
// dialect. See pkg/speechkit/stt.LanguageMulti.
const LanguageMulti = pkgstt.LanguageMulti

// IsMultilanguage reports whether a language value means "do not pin".
var IsMultilanguage = pkgstt.IsMultilanguage

// Functions and constructors (function-value aliases keep signatures in sync
// with the public package automatically).
var (
	ResolveTranscribeOptions    = pkgstt.ResolveTranscribeOptions
	ParseDeepgramKeyterms       = pkgstt.ParseDeepgramKeyterms
	ValidateModelPath           = pkgstt.ValidateModelPath
	FindWhisperBinary           = pkgstt.FindWhisperBinary
	SetSecretResolver           = pkgstt.SetSecretResolver
	NewGoogleSTTProvider        = pkgstt.NewGoogleSTTProvider
	NewDeepgramProvider         = pkgstt.NewDeepgramProvider
	NewAssemblyAIProvider       = pkgstt.NewAssemblyAIProvider
	NewHuggingFaceProvider      = pkgstt.NewHuggingFaceProvider
	NewOpenAISTTProvider        = pkgstt.NewOpenAISTTProvider
	NewOpenRouterSTTProvider    = pkgstt.NewOpenRouterSTTProvider
	NewGroqSTTProvider          = pkgstt.NewGroqSTTProvider
	NewLocalProvider            = pkgstt.NewLocalProvider
	NewVPSProvider              = pkgstt.NewVPSProvider
	NewVPSProviderWithModel     = pkgstt.NewVPSProviderWithModel
	NewOpenAICompatibleProvider = pkgstt.NewOpenAICompatibleProvider
	NewOllamaSTTProvider        = pkgstt.NewOllamaSTTProvider
)

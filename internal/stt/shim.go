package stt

// This file re-exports the STT adapters that now live in the public
// pkg/speechkit/stt package, so the existing call sites in cmd/speechkit,
// internal/server, internal/router, internal/serverclient, tools, etc. keep
// compiling unchanged. The Build factory (registry.go) keeps its host-facing
// BuildSpec typed with the internal models.ExecutionMode and forwards to the
// public registry in pkg/speechkit/stt. New adapter and assembly code goes in
// pkg/speechkit/stt.

import (
	pkgstt "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/allproviders"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/google"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/huggingface"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openrouter"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/vps"
)

// Router assembly (single public assembly path shared by the Device- and
// Server-Targets; see pkg/speechkit/stt.BuildRouter).
type (
	RouterConfig     = allproviders.RouterConfig
	EnabledProviders = allproviders.EnabledProviders
	LocalOpts        = allproviders.LocalOpts
	VPSOpts          = allproviders.VPSOpts
	HuggingFaceOpts  = allproviders.HuggingFaceOpts
	OpenAIOpts       = allproviders.OpenAIOpts
	GroqOpts         = allproviders.GroqOpts
	GoogleOpts       = allproviders.GoogleOpts
	DeepgramOpts     = allproviders.DeepgramOpts
	AssemblyAIOpts   = allproviders.AssemblyAIOpts
	OpenRouterOpts   = allproviders.OpenRouterOpts
	OllamaOpts       = allproviders.OllamaOpts
	FoundryOpts      = allproviders.FoundryOpts
)

// BuildRouter forwards to the public router assembly SSOT.
var BuildRouter = allproviders.BuildRouter

// Core contracts.
type (
	STTProvider               = pkgstt.STTProvider
	TranscribeOpts            = pkgstt.TranscribeOpts
	Result                    = pkgstt.Result
	WordConfidence            = pkgstt.WordConfidence
	ResolvedTranscribeOptions = pkgstt.ResolvedTranscribeOptions
	CapabilityReporter        = pkgstt.CapabilityReporter
	InstallStatus             = local.InstallStatus
)

// Concrete provider types.
type (
	GoogleSTTProvider        = google.Provider
	DeepgramProvider         = deepgram.Provider
	DeepgramOptions          = deepgram.Options
	AssemblyAIProvider       = assemblyai.Provider
	HuggingFaceProvider      = huggingface.Provider
	LocalProvider            = local.Provider
	OpenAICompatibleProvider = openaicompat.Provider
	OpenRouterSTTProvider    = openrouter.Provider
	VPSProvider              = vps.Provider
)

const MinWhisperModelBytes = local.MinWhisperModelBytes

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
	ParseDeepgramKeyterms       = deepgram.ParseKeyterms
	ValidateModelPath           = local.ValidateModelPath
	FindWhisperBinary           = local.FindWhisperBinary
	NewGoogleSTTProvider        = google.New
	NewDeepgramProvider         = deepgram.New
	NewAssemblyAIProvider       = assemblyai.New
	NewHuggingFaceProvider      = huggingface.New
	NewOpenAISTTProvider        = openaicompat.NewOpenAI
	NewOpenRouterSTTProvider    = openrouter.New
	NewGroqSTTProvider          = openaicompat.NewGroq
	NewLocalProvider            = local.New
	NewVPSProvider              = vps.New
	NewVPSProviderWithModel     = vps.NewWithModel
	NewOpenAICompatibleProvider = openaicompat.New
	NewOllamaSTTProvider        = openaicompat.NewOllama
)

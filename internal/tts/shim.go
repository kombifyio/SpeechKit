// Package tts re-exports the public pkg/speechkit/tts TTS surface so the
// existing kernel/adapter call sites (cmd/speechkit, internal/server,
// internal/assist, internal/voiceagent/cascaded, internal/ttswiring) keep
// compiling unchanged after the providers moved out to the public package.
// New TTS adapter code goes in pkg/speechkit/tts.
package tts

import pkgtts "github.com/kombifyio/SpeechKit/pkg/speechkit/tts"

// Core contracts and routing.
type (
	Provider                  = pkgtts.Provider
	ProviderKind              = pkgtts.ProviderKind
	SynthesizeOpts            = pkgtts.SynthesizeOpts
	ResolvedSynthesizeOptions = pkgtts.ResolvedSynthesizeOptions
	Result                    = pkgtts.Result
	Strategy                  = pkgtts.Strategy
	Router                    = pkgtts.Router
	Service                   = pkgtts.Service
	ServiceOption             = pkgtts.ServiceOption
	CapabilityReporter        = pkgtts.CapabilityReporter
	EnabledProviders          = pkgtts.EnabledProviders
)

// Concrete providers + their option structs.
type (
	OpenAI          = pkgtts.OpenAI
	OpenAIOpts      = pkgtts.OpenAIOpts
	Google          = pkgtts.Google
	GoogleOpts      = pkgtts.GoogleOpts
	Deepgram        = pkgtts.Deepgram
	DeepgramOpts    = pkgtts.DeepgramOpts
	HuggingFace     = pkgtts.HuggingFace
	HuggingFaceOpts = pkgtts.HuggingFaceOpts
	Foundry         = pkgtts.Foundry
	FoundryOpts     = pkgtts.FoundryOpts
	Piper           = pkgtts.Piper
	PiperOpts       = pkgtts.PiperOpts
	PiperVoiceInfo  = pkgtts.PiperVoiceInfo
)

const (
	ProviderKindLocalBuiltIn   = pkgtts.ProviderKindLocalBuiltIn
	ProviderKindLocalProvider  = pkgtts.ProviderKindLocalProvider
	ProviderKindCloudProvider  = pkgtts.ProviderKindCloudProvider
	ProviderKindDirectProvider = pkgtts.ProviderKindDirectProvider

	StrategyCloudFirst = pkgtts.StrategyCloudFirst
	StrategyLocalFirst = pkgtts.StrategyLocalFirst
	StrategyCloudOnly  = pkgtts.StrategyCloudOnly
	StrategyLocalOnly  = pkgtts.StrategyLocalOnly
)

// ErrMissingRouter is re-exported for callers that branch on it.
var ErrMissingRouter = pkgtts.ErrMissingRouter

// Functions and constructors (function-value aliases keep signatures in sync).
var (
	NewRouter                     = pkgtts.NewRouter
	NewService                    = pkgtts.NewService
	WithDefaultOpts               = pkgtts.WithDefaultOpts
	OrderByPreferredProvider      = pkgtts.OrderByPreferredProvider
	PreferredProviderForProfileID = pkgtts.PreferredProviderForProfileID
	ResolveSynthesizeOptions      = pkgtts.ResolveSynthesizeOptions
	BuildRouter                   = pkgtts.BuildRouter
	NewOpenAI                     = pkgtts.NewOpenAI
	NewGoogle                     = pkgtts.NewGoogle
	NewDeepgram                   = pkgtts.NewDeepgram
	NewHuggingFace                = pkgtts.NewHuggingFace
	NewFoundry                    = pkgtts.NewFoundry
	NewPiper                      = pkgtts.NewPiper
	ListPiperVoices               = pkgtts.ListPiperVoices
)

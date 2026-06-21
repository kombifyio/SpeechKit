package stt

// This file re-exports the STT adapters that now live in the public
// pkg/speechkit/stt package, so the existing call sites in cmd/speechkit,
// internal/server, internal/router, internal/serverclient, tools, etc. keep
// compiling unchanged. The Build factory (registry.go) deliberately stays in
// this internal shim because it maps the host's internal models.ExecutionMode
// onto the public constructors. New adapter code goes in pkg/speechkit/stt.

import pkgstt "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

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

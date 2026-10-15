// Package sttbatteries is the batteries-included models.STTBuilder: it hands a
// provider-neutral models.STTBuildSpec to pkg/speechkit/stt/allproviders, which
// links every STT provider SpeechKit ships. Only composition roots that offer a
// provider choice at runtime (the reference client, the server) import it; a
// managed root injects a builder with its allowed providers instead. Router
// is the matching router builder (desktop/providers.RouterBuilder).
package sttbatteries

import (
	"context"

	"github.com/kombifyio/SpeechKit/app/internal/models"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/allproviders"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
)

// Builder implements models.STTBuilder over allproviders.Build.
type Builder struct{}

var _ models.STTBuilder = Builder{}

// Build implements models.STTBuilder.
func (Builder) Build(spec models.STTBuildSpec) (string, stt.STTProvider, error) {
	bs := allproviders.BuildSpec{
		ExecutionMode:    spec.ExecutionMode,
		Provider:         spec.Provider,
		ModelID:          spec.ModelID,
		APIKey:           spec.APIKey,
		Token:            spec.Token,
		BaseURL:          spec.BaseURL,
		DiarizationModel: spec.DiarizationModel,
		Deepgram:         deepgram.Tuning(spec.Deepgram),
		BearerToken:      spec.BearerToken,

		GoogleStreamingCredentialsEnv:   spec.GoogleStreamingCredentialsEnv,
		GoogleApplicationCredentialsEnv: spec.GoogleApplicationCredentialsEnv,
		GoogleRegion:                    spec.GoogleRegion,
	}
	if f := spec.FoundrySpeech; f != nil {
		bs.FoundrySpeech = &allproviders.FoundrySpeechOpts{
			APIKey:      f.APIKey,
			BearerToken: f.BearerToken,
			Host:        f.Host,
			Model:       f.Model,
			Style:       f.Style,
			Diarization: f.Diarization,
		}
	}
	return allproviders.Build(bs)
}

// Router builds STT routers from a models.STTRouterSpec with every provider
// SpeechKit ships (allproviders.BuildRouter). It is the reference client's
// router builder; a managed root injects one that knows only the providers
// it lists.
type Router struct{}

// BuildRouter assembles the router spec describes. A spec without any
// provider yields an empty router that carries the routing knobs, so later
// provider refreshes have a target.
func (Router) BuildRouter(_ context.Context, spec models.STTRouterSpec) (*stt.Router, error) {
	routerCfg := allproviders.RouterConfig{
		Strategy:             spec.Strategy,
		PreferLocalUnderSecs: spec.PreferLocalUnderSecs,
		ParallelCloud:        spec.ParallelCloud,
		ReplaceOnBetter:      spec.ReplaceOnBetter,
		OnProviderSelected:   spec.OnProviderSelected,
	}
	r, ok, _ := allproviders.BuildRouter(routerCfg, enabledProviders(spec))
	if !ok {
		r = &stt.Router{
			Strategy:             routerCfg.Strategy,
			PreferLocalUnderSecs: routerCfg.PreferLocalUnderSecs,
			ParallelCloud:        routerCfg.ParallelCloud,
			ReplaceOnBetter:      routerCfg.ReplaceOnBetter,
			OnProviderSelected:   routerCfg.OnProviderSelected,
		}
	}
	return r, nil
}

func enabledProviders(spec models.STTRouterSpec) allproviders.EnabledProviders {
	enabled := allproviders.EnabledProviders{Secrets: spec.Secrets}
	if o := spec.Local; o != nil {
		enabled.Local = &allproviders.LocalOpts{Port: o.Port, ModelPath: o.ModelPath, GPU: o.GPU}
	}
	if o := spec.HuggingFace; o != nil {
		enabled.HuggingFace = &allproviders.HuggingFaceOpts{Model: o.Model, Token: o.APIKey}
	}
	if o := spec.OpenRouter; o != nil {
		enabled.OpenRouter = &allproviders.OpenRouterOpts{APIKey: o.APIKey, Model: o.Model}
	}
	if o := spec.VPS; o != nil {
		enabled.VPS = &allproviders.VPSOpts{URL: o.URL, APIKey: o.APIKey, Model: o.Model, Validation: o.Validation}
	}
	if o := spec.Ollama; o != nil {
		enabled.Ollama = &allproviders.OllamaOpts{BaseURL: o.URL, Model: o.Model, Validation: o.Validation}
	}
	if o := spec.Groq; o != nil {
		enabled.Groq = &allproviders.GroqOpts{APIKey: o.APIKey, Model: o.Model}
	}
	if o := spec.OpenAI; o != nil {
		enabled.OpenAI = &allproviders.OpenAIOpts{APIKey: o.APIKey, Model: o.Model}
	}
	if o := spec.Deepgram; o != nil {
		enabled.Deepgram = &allproviders.DeepgramOpts{
			APIKey:           o.APIKey,
			Model:            o.Model,
			DiarizationModel: o.DiarizationModel,
			Listen:           deepgram.Tuning(o.Listen),
		}
	}
	if o := spec.AssemblyAI; o != nil {
		enabled.AssemblyAI = &allproviders.AssemblyAIOpts{
			APIKey:            o.APIKey,
			Models:            o.Models,
			StreamingModel:    o.StreamingModel,
			StreamingBaseURL:  o.StreamingBaseURL,
			SyncBaseURL:       o.SyncBaseURL,
			DisableSync:       o.DisableSync,
			StreamingLLM:      o.StreamingLLM,
			StreamingLLMModel: o.StreamingLLMModel,
		}
	}
	if o := spec.Google; o != nil {
		enabled.Google = &allproviders.GoogleOpts{
			APIKey:                    o.APIKey,
			Model:                     o.Model,
			CredentialsJSONEnv:        o.CredentialsJSONEnv,
			ApplicationCredentialsEnv: o.ApplicationCredentialsEnv,
			Region:                    o.Region,
		}
	}
	if o := spec.Foundry; o != nil {
		enabled.Foundry = &allproviders.FoundryOpts{APIKey: o.APIKey, BearerToken: o.BearerToken, BaseURL: o.BaseURL, Model: o.Model}
	}
	if o := spec.FoundrySpeech; o != nil {
		enabled.FoundrySpeech = &allproviders.FoundrySpeechOpts{
			APIKey:      o.APIKey,
			BearerToken: o.BearerToken,
			Host:        o.Host,
			Model:       o.Model,
			Style:       o.Style,
			Diarization: o.Diarization,
		}
	}
	return enabled
}

// EnabledSet lets the provider policy (providerpolicy.EnforceSTT) drop
// providers from an allproviders.EnabledProviders a host filled, before
// allproviders.BuildRouter constructs them. Extra providers are named by
// their Name.
type EnabledSet struct {
	Enabled *allproviders.EnabledProviders
}

// Adapters lists the adapter ids the set carries, in assembly order.
func (s EnabledSet) Adapters() []string {
	e := s.Enabled
	if e == nil {
		return nil
	}
	var ids []string
	add := func(id string, present bool) {
		if present {
			ids = append(ids, id)
		}
	}
	add("local", e.Local != nil)
	add("huggingface", e.HuggingFace != nil)
	add("openrouter", e.OpenRouter != nil)
	add("vps", e.VPS != nil)
	add("ollama", e.Ollama != nil)
	add("groq", e.Groq != nil)
	add("openai", e.OpenAI != nil)
	add("deepgram", e.Deepgram != nil)
	add("assemblyai", e.AssemblyAI != nil)
	add("google", e.Google != nil)
	add("foundry", e.Foundry != nil || e.FoundrySpeech != nil)
	for _, p := range e.Extra {
		if p != nil {
			ids = append(ids, p.Name())
		}
	}
	return ids
}

// Drop removes the provider of adapter, named or extra.
func (s EnabledSet) Drop(adapter string) {
	e := s.Enabled
	if e == nil {
		return
	}
	switch adapter {
	case "local":
		e.Local = nil
	case "huggingface":
		e.HuggingFace = nil
	case "openrouter":
		e.OpenRouter = nil
	case "vps":
		e.VPS = nil
	case "ollama":
		e.Ollama = nil
	case "groq":
		e.Groq = nil
	case "openai":
		e.OpenAI = nil
	case "deepgram":
		e.Deepgram = nil
	case "assemblyai":
		e.AssemblyAI = nil
	case "google":
		e.Google = nil
	case "foundry":
		e.Foundry = nil
		e.FoundrySpeech = nil
	}
	extra := e.Extra[:0:0]
	for _, p := range e.Extra {
		if p != nil && p.Name() != adapter {
			extra = append(extra, p)
		}
	}
	e.Extra = extra
}

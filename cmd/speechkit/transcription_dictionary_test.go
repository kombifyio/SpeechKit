package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type captureSTTProvider struct {
	result *stt.Result
	opts   stt.TranscribeOpts
}

func (p *captureSTTProvider) Transcribe(_ context.Context, _ []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	p.opts = opts
	return p.result, nil
}

func (p *captureSTTProvider) Name() string { return "capture" }

func (p *captureSTTProvider) Health(context.Context) error { return nil }

type recordingUserDictionaryStore struct {
	records []string
}

func (s *recordingUserDictionaryStore) ReplaceUserDictionaryEntries(context.Context, string, []store.UserDictionaryEntry) error {
	return nil
}

func (s *recordingUserDictionaryStore) ListUserDictionaryEntries(context.Context, string) ([]store.UserDictionaryEntry, error) {
	return nil, nil
}

func (s *recordingUserDictionaryStore) RecordUserDictionaryUsage(_ context.Context, canonical, language string) error {
	s.records = append(s.records, canonical+"|"+language)
	return nil
}

func TestRouterTranscriberAppliesVocabularyHintsAndCorrections(t *testing.T) {
	provider := &captureSTTProvider{
		result: &stt.Result{
			Text:     "please call Kombi Fire tomorrow",
			Language: "en",
			Provider: "local",
			Model:    "whisper.cpp",
		},
	}
	r := &router.Router{Strategy: router.StrategyLocalOnly}
	r.SetLocal(provider)
	dictionaryStore := &recordingUserDictionaryStore{}

	transcriber := routerTranscriber{
		router:          r,
		dictionaryStore: dictionaryStore,
		state: &appState{
			vocabularyDictionary: "kombi fire => Kombify\nAcmeOS\nGemma",
		},
	}

	transcript, err := transcriber.Transcribe(context.Background(), []byte("wav"), 3.2, "en")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	if got, want := transcript.Text, "please call Kombify tomorrow"; got != want {
		t.Fatalf("transcript.Text = %q, want %q", got, want)
	}
	if got := provider.opts.Prompt; got == "" {
		t.Fatal("provider prompt = empty, want vocabulary hint prompt")
	}
	if got, want := provider.opts.Prompt, "Prefer these terms when transcribing: Kombify, AcmeOS, Gemma."; got != want {
		t.Fatalf("provider prompt = %q, want %q", got, want)
	}
	if got, want := dictionaryStore.records, []string{"Kombify|en"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("dictionary usage records = %v, want %v", got, want)
	}
}

func TestBuildVoiceAgentVocabularyHintUsesCanonicalTerms(t *testing.T) {
	entries := parseVocabularyDictionary("kombi fire => Kombify\nAcmeOS\nGemma\nacmeos")

	if got, want := buildVoiceAgentVocabularyHint(entries), "Prefer these names and product terms in recognition and responses: Kombify, AcmeOS, Gemma."; got != want {
		t.Fatalf("buildVoiceAgentVocabularyHint() = %q, want %q", got, want)
	}
}

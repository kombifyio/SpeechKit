package main

import (
	"context"
	"sync"
	"testing"
	"time"

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
	mu      sync.Mutex
	records []string
	entries []store.UserDictionaryEntry
}

func (s *recordingUserDictionaryStore) ReplaceUserDictionaryEntries(context.Context, string, []store.UserDictionaryEntry) error {
	return nil
}

func (s *recordingUserDictionaryStore) ListUserDictionaryEntries(context.Context, string) ([]store.UserDictionaryEntry, error) {
	return append([]store.UserDictionaryEntry(nil), s.entries...), nil
}

func (s *recordingUserDictionaryStore) RecordUserDictionaryUsage(_ context.Context, canonical, language string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, canonical+"|"+language)
	return nil
}

func (s *recordingUserDictionaryStore) recordsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.records...)
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
	wantRecords := []string{"Kombify|en"}
	deadline := time.After(time.Second)
	for {
		got := dictionaryStore.recordsSnapshot()
		if len(got) == len(wantRecords) && got[0] == wantRecords[0] {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("dictionary usage records = %v, want %v", got, wantRecords)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRouterTranscriberPrefersStoredVocabularyDictionary(t *testing.T) {
	provider := &captureSTTProvider{
		result: &stt.Result{
			Text:     "bitte kombi fire starten",
			Language: "de",
			Provider: "local",
			Model:    "whisper.cpp",
		},
	}
	r := &router.Router{Strategy: router.StrategyLocalOnly}
	r.SetLocal(provider)
	dictionaryStore := &recordingUserDictionaryStore{
		entries: []store.UserDictionaryEntry{
			{
				Spoken:    "kombi fire",
				Canonical: "Kombify",
				Language:  "de",
				Enabled:   true,
			},
		},
	}

	transcriber := routerTranscriber{
		router:          r,
		dictionaryStore: dictionaryStore,
		state: &appState{
			vocabularyDictionary: "legacy => Legacy",
		},
	}

	transcript, err := transcriber.Transcribe(context.Background(), []byte("wav"), 3.2, "de-DE")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	if got, want := transcript.Text, "bitte Kombify starten"; got != want {
		t.Fatalf("transcript.Text = %q, want %q", got, want)
	}
	if got, want := provider.opts.Prompt, "Prefer these terms when transcribing: Kombify."; got != want {
		t.Fatalf("provider prompt = %q, want %q", got, want)
	}
	wantRecords := []string{"Kombify|de"}
	deadline := time.After(time.Second)
	for {
		got := dictionaryStore.recordsSnapshot()
		if len(got) == len(wantRecords) && got[0] == wantRecords[0] {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("dictionary usage records = %v, want %v", got, wantRecords)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

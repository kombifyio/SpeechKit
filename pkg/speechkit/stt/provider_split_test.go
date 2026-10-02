package stt_test

import (
	"testing"

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

// TestProviderPackagesSatisfyTheContract pins what the per-provider split
// promises an embedder: every provider package constructs something the
// runtime accepts, and reports the provider identity the catalog names. A
// package that loses its constructor, or renames the provider it reports,
// fails here instead of in a consumer's build.
func TestProviderPackagesSatisfyTheContract(t *testing.T) {
	cases := []struct {
		path string
		want string
		got  stt.STTProvider
	}{
		{"stt/google.New", "google", google.New(google.Options{APIKey: "k", Model: "latest_long"})},
		{"stt/deepgram.New", "deepgram", deepgram.New(deepgram.Options{APIKey: "k", Model: "nova-3"})},
		{"stt/assemblyai.New", "assemblyai", assemblyai.New(assemblyai.Options{APIKey: "k", Models: "universal"})},
		{"stt/huggingface.New", "huggingface", huggingface.New(huggingface.Options{Model: "m", APIKey: "t"})},
		{"stt/openrouter.New", "openrouter", openrouter.New(openrouter.Options{APIKey: "k", Model: "m"})},
		{"stt/openaicompat.NewOpenAI", "openai", openaicompat.NewOpenAI(openaicompat.Options{APIKey: "k"})},
		{"stt/openaicompat.NewGroq", "groq", openaicompat.NewGroq(openaicompat.Options{APIKey: "k"})},
		{"stt/openaicompat.NewOllama", "ollama", openaicompat.NewOllama(openaicompat.Options{BaseURL: "http://h:1", Model: "m"})},
		{"stt/vps.New", "vps", vps.New(vps.Options{BaseURL: "http://h:1", APIKey: "k"})},
		{"stt/local.New", "local", local.New(local.Options{Port: 1, ModelPath: "p"})},
	}

	for _, tc := range cases {
		if tc.got == nil {
			t.Errorf("%s returned nil", tc.path)
			continue
		}
		if got := tc.got.Name(); got != tc.want {
			t.Errorf("%s reports provider %q, want %q", tc.path, got, tc.want)
		}
	}
}

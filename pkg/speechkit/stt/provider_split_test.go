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
		{"stt/google.New", "google", google.New("k", "latest_long")},
		{"stt/deepgram.New", "deepgram", deepgram.New("k", "nova-3")},
		{"stt/assemblyai.New", "assemblyai", assemblyai.New("k", "universal")},
		{"stt/huggingface.New", "huggingface", huggingface.New("m", "t")},
		{"stt/openrouter.New", "openrouter", openrouter.New("k", "m")},
		{"stt/openaicompat.NewOpenAI", "openai", openaicompat.NewOpenAI("k")},
		{"stt/openaicompat.NewGroq", "groq", openaicompat.NewGroq("k")},
		{"stt/openaicompat.NewOllama", "ollama", openaicompat.NewOllama("http://h:1", "m")},
		{"stt/vps.New", "vps", vps.New("http://h:1", "k")},
		{"stt/local.New", "local", local.New(1, "p", "")},
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

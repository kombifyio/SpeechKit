package stt_test

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
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

// TestProviderSubpackagesBuildTheSameProviders pins the v0.64 migration
// window: every provider reachable through the new per-provider import path
// must construct the same provider the deprecated root name constructs, and
// must report the same identity. A subpackage that forgets to re-export a
// constructor, or re-exports the wrong one, fails here rather than in a
// consumer's build after v0.65 removes the old names.
func TestProviderSubpackagesBuildTheSameProviders(t *testing.T) {
	cases := []struct {
		path string
		got  stt.STTProvider
		want stt.STTProvider
	}{
		{"stt/google.New", google.New("k", "latest_long"), stt.NewGoogleSTTProvider("k", "latest_long")},                        //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/deepgram.New", deepgram.New("k", "nova-3"), stt.NewDeepgramProvider("k", "nova-3")},                               //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/assemblyai.New", assemblyai.New("k", "universal"), stt.NewAssemblyAIProvider("k", "universal")},                   //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/huggingface.New", huggingface.New("m", "t"), stt.NewHuggingFaceProvider("m", "t")},                                //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/openrouter.New", openrouter.New("k", "m"), stt.NewOpenRouterSTTProvider("k", "m")},                                //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/openaicompat.NewOpenAI", openaicompat.NewOpenAI("k"), stt.NewOpenAISTTProvider("k")},                              //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/openaicompat.NewGroq", openaicompat.NewGroq("k"), stt.NewGroqSTTProvider("k")},                                    //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/openaicompat.NewOllama", openaicompat.NewOllama("http://h:1", "m"), stt.NewOllamaSTTProvider("http://h:1", "m")},  //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/vps.New", vps.New("http://h:1", "k"), stt.NewVPSProvider("http://h:1", "k")},                                      //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/vps.NewWithModel", vps.NewWithModel("http://h:1", "k", "m"), stt.NewVPSProviderWithModel("http://h:1", "k", "m")}, //nolint:staticcheck // deprecated name is the comparison subject
		{"stt/local.New", local.New(1, "p", ""), stt.NewLocalProvider(1, "p", "")},                                              //nolint:staticcheck // deprecated name is the comparison subject
	}

	for _, tc := range cases {
		if tc.got == nil {
			t.Errorf("%s returned nil", tc.path)
			continue
		}
		if got, want := tc.got.Name(), tc.want.Name(); got != want {
			t.Errorf("%s reports provider %q, root name reports %q", tc.path, got, want)
		}
	}
}

// TestAllProvidersAssemblesTheSameRouter pins the batteries package against
// the root assembly it forwards to during the migration window.
func TestAllProvidersAssemblesTheSameRouter(t *testing.T) {
	enabled := allproviders.EnabledProviders{
		Deepgram: &allproviders.DeepgramOpts{APIKey: "k", Model: "nova-3"},
		OpenAI:   &allproviders.OpenAIOpts{APIKey: "k"},
	}
	router, ok, notes := allproviders.BuildRouter(allproviders.RouterConfig{Strategy: stt.StrategyCloudOnly}, enabled)
	if !ok || router == nil {
		t.Fatal("expected a router for two enabled cloud providers")
	}
	if got := len(router.Providers()); got != 2 {
		t.Fatalf("router carries %d providers, want 2", got)
	}
	if len(notes) == 0 {
		t.Error("expected assembly notes describing the registered providers")
	}
}

package deepgram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func newNoStoreTestServer(t *testing.T, gotQuery *url.Values) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode(deepgramResponse{
			Results: deepgramResults{
				Channels: []deepgramChannel{
					{Alternatives: []deepgramAlternative{{Transcript: "hello", Confidence: 0.9}}},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// Opting out of the Model Improvement Partnership Program is what keeps the
// audio and the transcript from outliving the request at Deepgram, so it has
// to reach the wire rather than only the manifest.
func TestDeepgram_Transcribe_SendsMipOptOutForANoStoreRequest(t *testing.T) {
	var gotQuery url.Values
	server := newNoStoreTestServer(t, &gotQuery)

	p := newTestDeepgramProvider(server.URL)
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{
		Options: provideropts.Values{provideropts.OptionNoStore: true},
	}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Get("mip_opt_out") != "true" {
		t.Fatalf("mip_opt_out = %q, want true (query=%s)", gotQuery.Get("mip_opt_out"), gotQuery.Encode())
	}
}

// The provider-level switch is what a host retention policy sets, and it must
// hold for every request rather than only for those that ask.
func TestDeepgram_Transcribe_SendsMipOptOutForAProviderConfiguredForNoStore(t *testing.T) {
	var gotQuery url.Values
	server := newNoStoreTestServer(t, &gotQuery)

	p := newTestDeepgramProvider(server.URL)
	p.NoStore = true
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Get("mip_opt_out") != "true" {
		t.Fatalf("mip_opt_out = %q, want true (query=%s)", gotQuery.Get("mip_opt_out"), gotQuery.Encode())
	}
}

// Opting out is a policy choice with a price at Deepgram, so it must never be
// sent by itself.
func TestDeepgram_Transcribe_OmitsMipOptOutByDefault(t *testing.T) {
	var gotQuery url.Values
	server := newNoStoreTestServer(t, &gotQuery)

	p := newTestDeepgramProvider(server.URL)
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotQuery.Has("mip_opt_out") {
		t.Fatalf("mip_opt_out was sent without being asked for: %s", gotQuery.Encode())
	}
}

// The streaming paths carry no resolved options, so they follow the provider
// switch. A meeting or a live dictation must not be the one request that keeps
// its audio at the vendor.
func TestDeepgram_StreamingEndpoints_CarryMipOptOut(t *testing.T) {
	p := newTestDeepgramProvider("https://api.deepgram.example")
	p.NoStore = true

	endpoint, err := p.deepgramStreamingEndpoint("nova-3", "de", speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("deepgramStreamingEndpoint: %v", err)
	}
	assertQueryHasMipOptOut(t, "streaming", endpoint)

	flux, err := p.deepgramFluxEndpoint(DeepgramFluxModelEN, FluxStreamOptions{}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("deepgramFluxEndpoint: %v", err)
	}
	assertQueryHasMipOptOut(t, "flux", flux)
}

func assertQueryHasMipOptOut(t *testing.T, name, endpoint string) {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse %s endpoint: %v", name, err)
	}
	if parsed.Query().Get("mip_opt_out") != "true" {
		t.Fatalf("%s endpoint is missing mip_opt_out: %s", name, parsed.RawQuery)
	}
}

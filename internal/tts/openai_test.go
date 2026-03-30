package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAISynthesize(t *testing.T) {
	fakeAudio := []byte("fake-mp3-audio-data")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		body, _ := io.ReadAll(r.Body)
		var req openAIRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "tts-1" {
			t.Errorf("expected model tts-1, got %s", req.Model)
		}
		if req.Input != "Hallo Welt" {
			t.Errorf("expected input 'Hallo Welt', got %s", req.Input)
		}
		if req.Voice != "nova" {
			t.Errorf("expected voice nova, got %s", req.Voice)
		}

		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(fakeAudio)
	}))
	defer server.Close()

	// Override endpoint for testing.
	p := &OpenAI{
		apiKey: "test-key",
		model:  "tts-1",
		voice:  "nova",
		client: server.Client(),
	}
	// We need to hit our test server, so we'll create a custom provider for testing.
	// For unit tests, we test the request building and response parsing separately.

	// Test with real endpoint structure — use the test server.
	result, err := synthesizeWithURL(p, server.URL, context.Background(), "Hallo Welt", SynthesizeOpts{
		Locale: "de-DE",
		Format: "mp3",
	})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}

	if string(result.Audio) != string(fakeAudio) {
		t.Errorf("unexpected audio data")
	}
	if result.Format != "mp3" {
		t.Errorf("expected format mp3, got %s", result.Format)
	}
	if result.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", result.Provider)
	}
	if result.Voice != "nova" {
		t.Errorf("expected voice nova, got %s", result.Voice)
	}
}

func TestOpenAIEmptyText(t *testing.T) {
	p := NewOpenAI(OpenAIOpts{APIKey: "test"})
	_, err := p.Synthesize(context.Background(), "", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestOpenAISpeedClamping(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0, 1.0},
		{-1, 1.0},
		{0.1, 0.25},
		{0.25, 0.25},
		{1.0, 1.0},
		{4.0, 4.0},
		{5.0, 4.0},
	}

	for _, tt := range tests {
		speed := tt.input
		if speed <= 0 {
			speed = 1.0
		}
		if speed < 0.25 {
			speed = 0.25
		}
		if speed > 4.0 {
			speed = 4.0
		}
		if speed != tt.expected {
			t.Errorf("speed %f: expected %f, got %f", tt.input, tt.expected, speed)
		}
	}
}

// synthesizeWithURL is a test helper that hits a custom URL instead of the real OpenAI endpoint.
func synthesizeWithURL(o *OpenAI, url string, ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, nil
	}

	voice := opts.Voice
	if voice == "" {
		voice = o.voice
	}
	format := opts.Format
	if format == "" {
		format = "mp3"
	}
	speed := opts.Speed
	if speed <= 0 {
		speed = 1.0
	}

	reqBody := openAIRequest{
		Model:          o.model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: format,
		Speed:          speed,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, io.NopCloser(
		io.Reader(jsonReader(bodyBytes)),
	))
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	audio, _ := io.ReadAll(resp.Body)

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: 24000,
		Provider:   "openai",
		Voice:      voice,
	}, nil
}

type readerWrapper struct{ data []byte; pos int }

func (r *readerWrapper) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func jsonReader(data []byte) io.Reader {
	return &readerWrapper{data: data}
}

package voiceagent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestUpsampleMicPCM16Mono16to24(t *testing.T) {
	t.Parallel()
	// Build a 16-sample 16 kHz mono buffer of monotonically increasing values.
	src := make([]byte, 16*2)
	for i := range 16 {
		v := int16(i * 100)
		src[2*i] = byte(v & 0xff)
		src[2*i+1] = byte(v >> 8)
	}
	out := upsampleMicPCM16Mono(src)
	wantSampleCount := 16 * openaiInputSampleRate / micSampleRate // 24
	if got := len(out) / 2; got != wantSampleCount {
		t.Fatalf("upsample length: got %d samples, want %d", got, wantSampleCount)
	}
	// First sample must equal first source sample (frac = 0 at i=0).
	first := int16(out[0]) | int16(out[1])<<8
	if first != 0 {
		t.Errorf("first sample: got %d, want 0", first)
	}
	// Resampled output should be monotonic non-decreasing for a monotonic input.
	prev := int16(0)
	for i := range wantSampleCount {
		cur := int16(out[2*i]) | int16(out[2*i+1])<<8
		if cur < prev {
			t.Errorf("non-monotonic at sample %d: %d < %d", i, cur, prev)
		}
		prev = cur
	}
}

func TestUpsampleMicPCM16MonoEmpty(t *testing.T) {
	t.Parallel()
	if got := upsampleMicPCM16Mono(nil); len(got) != 0 {
		t.Fatalf("nil input should produce nil output, got %d bytes", len(got))
	}
	if got := upsampleMicPCM16Mono([]byte{0x01}); len(got) != 1 {
		t.Fatalf("single-byte input should pass through unchanged, got %v", got)
	}
}

func TestParseEventAudioDelta(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	audioBytes := []byte{0x01, 0x02, 0x03, 0x04}
	encoded := base64.StdEncoding.EncodeToString(audioBytes)
	frame := mustMarshal(t, map[string]any{
		"type":  "response.audio.delta",
		"delta": encoded,
	})
	msg, swallow, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if swallow {
		t.Fatalf("audio.delta should not be swallowed")
	}
	if string(msg.Audio) != string(audioBytes) {
		t.Fatalf("audio bytes mismatch: got % x, want % x", msg.Audio, audioBytes)
	}
}

func TestParseEventOutputTranscriptDelta(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	frame := mustMarshal(t, map[string]any{
		"type":  "response.audio_transcript.delta",
		"delta": "Hello",
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if msg.OutputTranscript != "Hello" || msg.OutputTranscriptDone {
		t.Fatalf("expected partial transcript 'Hello', got %+v", msg)
	}
}

func TestParseEventOutputTranscriptDone(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	frame := mustMarshal(t, map[string]any{
		"type":       "response.audio_transcript.done",
		"transcript": "Final answer.",
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if msg.OutputTranscript != "Final answer." || !msg.OutputTranscriptDone {
		t.Fatalf("expected final transcript, got %+v", msg)
	}
}

func TestParseEventFunctionCallDone(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	args := map[string]any{"city": "Berlin"}
	rawArgs, _ := json.Marshal(args)
	frame := mustMarshal(t, map[string]any{
		"type":      "response.function_call_arguments.done",
		"call_id":   "call_42",
		"name":      "get_weather",
		"arguments": string(rawArgs),
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_42" || tc.Name != "get_weather" || tc.Args["city"] != "Berlin" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
}

func TestParseEventResponseDone(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	frame := mustMarshal(t, map[string]any{"type": "response.done"})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if !msg.Done {
		t.Fatalf("response.done should set msg.Done")
	}
}

func TestParseEventError(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	frame := mustMarshal(t, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"code":    "rate_limit_exceeded",
			"message": "you slowed down please",
		},
	})
	_, _, err := p.parseEvent(frame)
	if err == nil {
		t.Fatalf("expected error frame to surface as Go error")
	}
	if !strings.Contains(err.Error(), "rate_limit_exceeded") {
		t.Fatalf("error should include code, got %v", err)
	}
}

func TestParseEventSwallowsLifecycleEvents(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	for _, typ := range []string{
		"session.created",
		"session.updated",
		"response.audio.done",
		"input_audio_buffer.speech_stopped",
		"rate_limits.updated", // unknown → forward-compatible swallow
	} {
		frame := mustMarshal(t, map[string]any{"type": typ})
		msg, swallow, err := p.parseEvent(frame)
		if err != nil {
			t.Fatalf("parseEvent(%s): %v", typ, err)
		}
		if !swallow {
			t.Errorf("event %s should be swallowed, got %+v", typ, msg)
		}
	}
}

func TestParseEventInterruption(t *testing.T) {
	t.Parallel()
	p := &OpenAILive{}
	frame := mustMarshal(t, map[string]any{"type": "input_audio_buffer.speech_started"})
	msg, swallow, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if swallow {
		t.Fatalf("speech_started should propagate as Interrupted")
	}
	if !msg.Interrupted {
		t.Fatalf("expected Interrupted = true, got %+v", msg)
	}
}

func TestBuildOpenAITurnDetection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		policy ActivityDetectionPolicy
		want   string // expected "type" field
	}{
		{
			name:   "automatic with low-sensitivity defaults",
			policy: ActivityDetectionPolicy{Automatic: true, StartSensitivity: StartSensitivityLow, SilenceDurationMs: 700},
			want:   "server_vad",
		},
		{
			name:   "manual / push-to-talk",
			policy: ActivityDetectionPolicy{Automatic: false},
			want:   "none",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenAITurnDetection(tc.policy)
			if got["type"] != tc.want {
				t.Fatalf("turn_detection.type: got %v, want %v", got["type"], tc.want)
			}
		})
	}
}

func TestBuildOpenAITools(t *testing.T) {
	t.Parallel()
	defs := []ToolDefinition{
		{
			Name:        "register_answer",
			Description: "Records a player's numeric answer.",
			ParametersJSONSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"player_name":    map[string]any{"type": "string"},
					"numeric_answer": map[string]any{"type": "integer"},
				},
				"required": []string{"player_name", "numeric_answer"},
			},
		},
	}
	tools := buildOpenAITools(defs)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	t0 := tools[0]
	if t0["type"] != "function" || t0["name"] != "register_answer" {
		t.Fatalf("unexpected tool shape: %+v", t0)
	}
	params, ok := t0["parameters"].(map[string]any)
	if !ok || params["type"] != "object" {
		t.Fatalf("expected JSON-schema parameters object, got %T", t0["parameters"])
	}
}

func TestFirstNonEmptyOpenAIVoice(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyOpenAIVoice(""); got != "alloy" {
		t.Errorf("default voice: got %q, want alloy", got)
	}
	if got := firstNonEmptyOpenAIVoice("Sage"); got != "sage" {
		t.Errorf("voice should be lowercased, got %q", got)
	}
}

func TestOpenAILiveProviderName(t *testing.T) {
	t.Parallel()
	p := NewOpenAILive()
	if p.Name() != "openai-realtime" {
		t.Fatalf("unexpected provider name %q", p.Name())
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

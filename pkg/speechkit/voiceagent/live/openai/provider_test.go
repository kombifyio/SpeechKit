package openai

import (
	"encoding/base64"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
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
	out := live.UpsampleMicPCM16Mono(src)
	wantSampleCount := 16 * openaiInputSampleRate / 16000 // 24
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
	if got := live.UpsampleMicPCM16Mono(nil); len(got) != 0 {
		t.Fatalf("nil input should produce nil output, got %d bytes", len(got))
	}
	if got := live.UpsampleMicPCM16Mono([]byte{0x01}); len(got) != 1 {
		t.Fatalf("single-byte input should pass through unchanged, got %v", got)
	}
}

func TestParseEventAudioDelta(t *testing.T) {
	t.Parallel()
	p := &Provider{}
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
	if msg.EventType != live.LiveEventOutputAudio || msg.ProviderMetadata["provider_event"] != "response.audio.delta" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

// A `response.cancel` that races an already-finished response makes the
// server emit an error event with code response_cancel_not_active. The session
// stays fully usable, so parseEvent must swallow it — treating it as fatal
// tears down the whole voice session for a harmless no-op cancel.
func TestParseEventSwallowsResponseCancelNotActive(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	frame := mustMarshal(t, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"code":    "response_cancel_not_active",
			"message": "Cancellation failed: no active response found",
		},
	})
	msg, swallow, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v, want swallowed no-op cancel error", err)
	}
	if !swallow || msg != nil {
		t.Fatalf("swallow=%v msg=%+v, want swallowed nil message", swallow, msg)
	}
}

func TestParseEventOutputAudioDeltaGA(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	audioBytes := []byte{0x05, 0x06, 0x07, 0x08}
	encoded := base64.StdEncoding.EncodeToString(audioBytes)
	frame := mustMarshal(t, map[string]any{
		"type":  "response.output_audio.delta",
		"delta": encoded,
	})
	msg, swallow, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if swallow {
		t.Fatalf("output_audio.delta should not be swallowed")
	}
	if string(msg.Audio) != string(audioBytes) {
		t.Fatalf("audio bytes mismatch: got % x, want % x", msg.Audio, audioBytes)
	}
	if msg.EventType != live.LiveEventOutputAudio || msg.ProviderMetadata["provider_event"] != "response.output_audio.delta" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventOutputTranscriptDelta(t *testing.T) {
	t.Parallel()
	p := &Provider{}
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
	if msg.EventType != live.LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "response.audio_transcript.delta" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventInputTranscriptEvents(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	partial, swallow, err := p.parseEvent(mustMarshal(t, map[string]any{
		"type":  "conversation.item.input_audio_transcription.delta",
		"delta": "Hal",
	}))
	if err != nil || swallow {
		t.Fatalf("input transcription delta: swallow=%v err=%v", swallow, err)
	}
	if partial.InputTranscript != "Hal" || partial.InputTranscriptDone || partial.EventType != live.LiveEventInputPartial {
		t.Fatalf("partial input transcript mismatch: %+v", partial)
	}

	final, swallow, err := p.parseEvent(mustMarshal(t, map[string]any{
		"type":       "conversation.item.input_audio_transcription.completed",
		"transcript": "Hallo",
	}))
	if err != nil || swallow {
		t.Fatalf("input transcription completed: swallow=%v err=%v", swallow, err)
	}
	if final.InputTranscript != "Hallo" || !final.InputTranscriptDone || final.EventType != live.LiveEventInputFinal {
		t.Fatalf("final input transcript mismatch: %+v", final)
	}
}

func TestParseEventOutputTranscriptDeltaGA(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	frame := mustMarshal(t, map[string]any{
		"type":  "response.output_audio_transcript.delta",
		"delta": "Hello",
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if msg.OutputTranscript != "Hello" || msg.OutputTranscriptDone {
		t.Fatalf("expected partial transcript 'Hello', got %+v", msg)
	}
	if msg.EventType != live.LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "response.output_audio_transcript.delta" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventOutputTranscriptDone(t *testing.T) {
	t.Parallel()
	p := &Provider{}
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
	if msg.EventType != live.LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "response.audio_transcript.done" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventOutputTranscriptDoneGA(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	frame := mustMarshal(t, map[string]any{
		"type":       "response.output_audio_transcript.done",
		"transcript": "Final answer.",
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if msg.OutputTranscript != "Final answer." || !msg.OutputTranscriptDone {
		t.Fatalf("expected final transcript, got %+v", msg)
	}
	if msg.EventType != live.LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "response.output_audio_transcript.done" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventOutputTextDeltaAlsoSetsTranscript(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	frame := mustMarshal(t, map[string]any{
		"type":  "response.output_text.delta",
		"delta": "Text answer",
	})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if msg.Text != "Text answer" || msg.OutputTranscript != "Text answer" {
		t.Fatalf("expected text delta to populate text and transcript, got %+v", msg)
	}
	if msg.EventType != live.LiveEventOutputText || msg.ProviderMetadata["provider_event"] != "response.output_text.delta" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventFunctionCallDone(t *testing.T) {
	t.Parallel()
	p := &Provider{}
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
	if msg.EventType != live.LiveEventToolCall || msg.ProviderMetadata["provider_event"] != "response.function_call_arguments.done" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventResponseDone(t *testing.T) {
	t.Parallel()
	p := &Provider{}
	frame := mustMarshal(t, map[string]any{"type": "response.done"})
	msg, _, err := p.parseEvent(frame)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if !msg.Done {
		t.Fatalf("response.done should set msg.Done")
	}
	if msg.EventType != live.LiveEventTurnEnd || msg.ProviderMetadata["provider_event"] != "response.done" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestParseEventError(t *testing.T) {
	t.Parallel()
	p := &Provider{}
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
	p := &Provider{}
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
	p := &Provider{}
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
	if msg.EventType != live.LiveEventInterrupted || msg.ProviderMetadata["provider_event"] != "input_audio_buffer.speech_started" {
		t.Fatalf("event metadata mismatch: %+v", msg)
	}
}

func TestBuildOpenAITurnDetection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		policy live.ActivityDetectionPolicy
		want   string // expected "type" field
	}{
		{
			name:   "automatic with low-sensitivity defaults",
			policy: live.ActivityDetectionPolicy{Automatic: true, StartSensitivity: live.StartSensitivityLow, SilenceDurationMs: 700},
			want:   "server_vad",
		},
		{
			name:   "manual / push-to-talk",
			policy: live.ActivityDetectionPolicy{Automatic: false},
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenAITurnDetection(tc.policy)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("turn_detection: got %v, want nil", got)
				}
				return
			}
			if got["type"] != tc.want {
				t.Fatalf("turn_detection.type: got %v, want %v", got["type"], tc.want)
			}
		})
	}
}

func TestBuildOpenAITools(t *testing.T) {
	t.Parallel()
	defs := []live.ToolDefinition{
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
	if got := firstNonEmptyOpenAIVoice("Kore"); got != "alloy" {
		t.Errorf("unknown non-OpenAI voice should fall back to alloy, got %q", got)
	}
}

func TestOpenAIRealtimeGAConnectionDefaults(t *testing.T) {
	t.Parallel()
	if got := resolveOpenAIRealtimeModel(""); got != "gpt-realtime-2" {
		t.Fatalf("default realtime model = %q, want gpt-realtime-2", got)
	}
	header := openAIRealtimeHeaders("test-key")
	if got := header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization header = %q", got)
	}
	if got := header.Get("OpenAI-Beta"); got != "" {
		t.Fatalf("GA realtime WebSocket must not send OpenAI-Beta header, got %q", got)
	}
}

func TestBuildOpenAISessionIncludesGAType(t *testing.T) {
	t.Parallel()
	session := buildOpenAISession(live.LiveConfig{
		Model: "gpt-realtime-2",
		Voice: "alloy",
		Policies: live.LivePolicies{
			EnableInputAudioTranscription: true,
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:        true,
				StartSensitivity: live.StartSensitivityLow,
			},
		},
	})
	if got := session["type"]; got != "realtime" {
		t.Fatalf("session.type = %v, want realtime", got)
	}
	if _, ok := session["input_audio_format"]; ok {
		t.Fatalf("GA realtime session must not include legacy input_audio_format")
	}
	if got := session["model"]; got != "gpt-realtime-2" {
		t.Fatalf("session.model = %v, want gpt-realtime-2", got)
	}
	modalities, ok := session["output_modalities"].([]string)
	if !ok || len(modalities) != 1 || modalities[0] != "audio" {
		t.Fatalf("session.output_modalities = %#v, want [audio]", session["output_modalities"])
	}
	audio, ok := session["audio"].(map[string]any)
	if !ok {
		t.Fatalf("session.audio = %T, want map", session["audio"])
	}
	input, ok := audio["input"].(map[string]any)
	if !ok {
		t.Fatalf("session.audio.input = %T, want map", audio["input"])
	}
	format, ok := input["format"].(map[string]any)
	if !ok || format["type"] != "audio/pcm" || format["rate"] != openaiInputSampleRate {
		t.Fatalf("session.audio.input.format = %#v", input["format"])
	}
	if _, ok := input["transcription"].(map[string]any); !ok {
		t.Fatalf("session.audio.input.transcription missing")
	}
	output, ok := audio["output"].(map[string]any)
	if !ok || output["voice"] != "alloy" {
		t.Fatalf("session.audio.output = %#v", audio["output"])
	}
	outputFormat, ok := output["format"].(map[string]any)
	if !ok || outputFormat["type"] != "audio/pcm" || outputFormat["rate"] != openaiInputSampleRate {
		t.Fatalf("session.audio.output.format = %#v", output["format"])
	}
}

func TestBuildOpenAISessionMapsReasoningEffort(t *testing.T) {
	t.Parallel()
	session := buildOpenAISession(live.LiveConfig{
		Model: "gpt-realtime-2",
		ProviderOptions: provideropts.Values{
			provideropts.OptionReasoningEffort: "high",
		},
	})
	reasoning, ok := session["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v", session["reasoning"])
	}
}

func TestOpenAILiveProviderName(t *testing.T) {
	t.Parallel()
	p := New()
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

func TestServerVADEnabledFromLastConfig(t *testing.T) {
	auto := live.LiveConfig{Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{Automatic: true}}}
	ptt := live.LiveConfig{Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{Automatic: false}}}

	p := &Provider{}
	if p.serverVADEnabled() {
		t.Fatal("serverVADEnabled must be false when no session config is set")
	}

	p.lastConfig = &auto
	if !p.serverVADEnabled() {
		t.Fatal("serverVADEnabled must be true when automatic activity detection is on")
	}

	p.lastConfig = &ptt
	if p.serverVADEnabled() {
		t.Fatal("serverVADEnabled must be false in push-to-talk (automatic detection off)")
	}
}

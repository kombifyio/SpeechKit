package voicelive_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/voicelive"
)

// Hosts branch on the shared live sentinels, so the provider must wrap them
// for the endpoint, the credential and the not-connected paths.
func TestConnectWrapsSharedSentinels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if err := voicelive.New().Connect(ctx, live.LiveConfig{APIKey: "k"}); !errors.Is(err, live.ErrMissingEndpoint) {
		t.Fatalf("Connect without Endpoint = %v, want ErrMissingEndpoint", err)
	}
	if err := voicelive.New().Connect(ctx, live.LiveConfig{Endpoint: "wss://res.services.ai.azure.com/voice-live/realtime"}); !errors.Is(err, live.ErrMissingAPIKey) {
		t.Fatalf("Connect without credential = %v, want ErrMissingAPIKey", err)
	}
	p := voicelive.New()
	if err := p.SendAudio([]byte{0, 0}); !errors.Is(err, live.ErrNotConnected) {
		t.Fatalf("SendAudio before Connect = %v, want ErrNotConnected", err)
	}
	if _, err := p.Receive(ctx); !errors.Is(err, live.ErrNotConnected) {
		t.Fatalf("Receive before Connect = %v, want ErrNotConnected", err)
	}
}

func TestProviderIdentity(t *testing.T) {
	t.Parallel()
	p := voicelive.New()
	if p.Name() != "foundry-voicelive" {
		t.Fatalf("Name = %q", p.Name())
	}
	if caps := p.SessionCapabilities(); caps.Provider != "foundry" {
		t.Fatalf("SessionCapabilities.Provider = %q, want foundry", caps.Provider)
	}
}

func TestDialURLCarriesAPIVersionAndModel(t *testing.T) {
	t.Parallel()
	p := voicelive.New()
	u := mustParseURL(t, p.DialURL("wss://res.services.ai.azure.com/voice-live/realtime", "gpt-5 mini"))
	if u.Path != "/voice-live/realtime" {
		t.Fatalf("path = %q", u.Path)
	}
	if got := u.Query().Get("api-version"); got != voicelive.DefaultAPIVersion {
		t.Fatalf("api-version = %q, want %q", got, voicelive.DefaultAPIVersion)
	}
	if got := u.Query().Get("model"); got != "gpt-5 mini" {
		t.Fatalf("model = %q, want the escaped model round-tripped", got)
	}

	p.APIVersion = "2026-01-01"
	u = mustParseURL(t, p.DialURL("wss://h/voice-live/realtime?x=1", "gpt-4.1"))
	if q := u.Query(); q.Get("api-version") != "2026-01-01" || q.Get("model") != "gpt-4.1" || q.Get("x") != "1" {
		t.Fatalf("query = %v, want api-version override, model and the endpoint's own parameter", q)
	}
}

// Auth is a registered sensitive boundary: the token source must win over the
// static key, its failure must surface, and the key must go into api-key.
func TestDialHeadersPreferBearerTokenOverKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := voicelive.New()

	header, err := p.DialHeaders(ctx, live.LiveConfig{APIKey: "resource-key"})
	if err != nil {
		t.Fatalf("api-key headers: %v", err)
	}
	if header.Get("api-key") != "resource-key" || header.Get("Authorization") != "" {
		t.Fatalf("api-key headers = %v", header)
	}

	header, err = p.DialHeaders(ctx, live.LiveConfig{
		APIKey:      "resource-key",
		BearerToken: func(context.Context) (string, error) { return "entra-token", nil },
	})
	if err != nil {
		t.Fatalf("bearer headers: %v", err)
	}
	if header.Get("Authorization") != "Bearer entra-token" || header.Get("api-key") != "" {
		t.Fatalf("bearer headers = %v, want bearer only", header)
	}

	wantErr := errors.New("token expired")
	if _, err := p.DialHeaders(ctx, live.LiveConfig{
		BearerToken: func(context.Context) (string, error) { return "", wantErr },
	}); !errors.Is(err, wantErr) {
		t.Fatalf("bearer failure = %v, want %v", err, wantErr)
	}
}

func TestBuildSessionTypicalConfig(t *testing.T) {
	t.Parallel()
	p := voicelive.New()
	p.VoiceStyle = "cheerful"
	cfg := live.LiveConfig{
		Voice:          "de-DE-Mia:MAI-Voice-2",
		Locale:         "de-DE",
		VocabularyHint: "Kombify, SpeechKit",
		Policies: live.LivePolicies{
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:         true,
				StartSensitivity:  live.StartSensitivityLow,
				SilenceDurationMs: 700,
			},
		},
		Tools: []live.ToolDefinition{{
			Name:        "register_answer",
			Description: "Records a player's numeric answer.",
			ParametersJSONSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"numeric_answer": map[string]any{"type": "integer"}},
			},
		}},
	}
	session := roundTrip(t, p.BuildSession(cfg, "gpt-realtime-2", "Be brief."))

	if _, ok := session["model"]; ok {
		t.Fatalf("session.model must stay server-owned (dial query), got %v", session["model"])
	}
	if session["instructions"] != "Be brief." {
		t.Fatalf("instructions = %v", session["instructions"])
	}
	if session["input_audio_format"] != "pcm16" || session["output_audio_format"] != "pcm16" {
		t.Fatalf("audio formats = %v / %v", session["input_audio_format"], session["output_audio_format"])
	}
	if modalities, _ := session["modalities"].([]any); len(modalities) != 2 || modalities[0] != "text" || modalities[1] != "audio" {
		t.Fatalf("modalities = %v", session["modalities"])
	}

	voice := object(t, session, "voice")
	if voice["name"] != "de-DE-Mia:MAI-Voice-2" || voice["type"] != "azure-standard" || voice["style"] != "cheerful" {
		t.Fatalf("voice = %v", voice)
	}
	if _, ok := voice["rate"]; ok {
		t.Fatalf("voice.rate must be omitted when unset, got %v", voice)
	}

	transcription := object(t, session, "input_audio_transcription")
	if transcription["model"] != voicelive.DefaultTranscriptionModel || transcription["language"] != "de-DE" {
		t.Fatalf("input_audio_transcription = %v", transcription)
	}
	if phrases, _ := transcription["phrase_list"].([]any); len(phrases) != 2 || phrases[0] != "Kombify" || phrases[1] != "SpeechKit" {
		t.Fatalf("phrase_list = %v", transcription["phrase_list"])
	}

	td := object(t, session, "turn_detection")
	if td["type"] != "azure_semantic_vad" || td["create_response"] != true || td["interrupt_response"] != true || td["remove_filler_words"] != false {
		t.Fatalf("turn_detection = %v", td)
	}
	if td["silence_duration_ms"] != float64(700) || td["threshold"] != 0.7 {
		t.Fatalf("turn_detection must keep the server_vad numeric mapping, got %v", td)
	}

	if object(t, session, "input_audio_noise_reduction")["type"] != "azure_deep_noise_suppression" {
		t.Fatalf("input_audio_noise_reduction = %v", session["input_audio_noise_reduction"])
	}
	if object(t, session, "input_audio_echo_cancellation")["type"] != "server_echo_cancellation" {
		t.Fatalf("input_audio_echo_cancellation = %v", session["input_audio_echo_cancellation"])
	}

	tools, _ := session["tools"].([]any)
	if len(tools) != 1 || session["tool_choice"] != "auto" {
		t.Fatalf("tools = %v, tool_choice = %v", session["tools"], session["tool_choice"])
	}
	if tool, _ := tools[0].(map[string]any); tool["type"] != "function" || tool["name"] != "register_answer" {
		t.Fatalf("tool = %v", tools[0])
	}
}

func TestBuildSessionTurnDetectionVariants(t *testing.T) {
	t.Parallel()
	pushToTalk := live.LiveConfig{Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{Automatic: false}}}
	automatic := live.LiveConfig{Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{Automatic: true}}}

	p := voicelive.New()
	session := roundTrip(t, p.BuildSession(pushToTalk, "m", ""))
	if td, ok := session["turn_detection"]; !ok || td != nil {
		t.Fatalf("push-to-talk turn_detection = %v (present=%v), want explicit null", td, ok)
	}

	p.SemanticVAD = false
	td := object(t, roundTrip(t, p.BuildSession(automatic, "m", "")), "turn_detection")
	if td["type"] != "server_vad" {
		t.Fatalf("server_vad turn_detection = %v", td)
	}
	if _, ok := td["remove_filler_words"]; ok {
		t.Fatalf("server_vad must not carry the semantic-VAD switch, got %v", td)
	}

	// Option overrides reach Voice Live the same way they reach OpenAI: an
	// endpointing override turns detection on, turn_detection=false off.
	p = voicelive.New()
	withEndpointing := pushToTalk
	withEndpointing.ProviderOptions = provideropts.Values{provideropts.OptionEndpointingMs: 300}
	td = object(t, roundTrip(t, p.BuildSession(withEndpointing, "m", "")), "turn_detection")
	if td["type"] != "azure_semantic_vad" || td["silence_duration_ms"] != float64(300) {
		t.Fatalf("endpointing override turn_detection = %v", td)
	}
	disabled := automatic
	disabled.ProviderOptions = provideropts.Values{provideropts.OptionTurnDetection: false}
	session = roundTrip(t, p.BuildSession(disabled, "m", ""))
	if td, ok := session["turn_detection"]; !ok || td != nil {
		t.Fatalf("turn_detection=false override = %v (present=%v), want explicit null", td, ok)
	}
}

func TestBuildSessionTranscriptionLanguageAndSwitch(t *testing.T) {
	t.Parallel()
	p := voicelive.New()
	for locale, want := range map[string]string{
		"de":    "de-DE",
		"en":    "en-US",
		"de-de": "de-DE",
		"pt-BR": "pt-BR",
		"auto":  "",
		"":      "",
	} {
		session := roundTrip(t, p.BuildSession(live.LiveConfig{Locale: locale}, "m", ""))
		got, _ := object(t, session, "input_audio_transcription")["language"].(string)
		if got != want {
			t.Errorf("locale %q: language = %q, want %q", locale, got, want)
		}
	}

	off := voicelive.New()
	off.TranscriptionModel = ""
	session := roundTrip(t, off.BuildSession(live.LiveConfig{}, "m", ""))
	if _, ok := session["input_audio_transcription"]; ok {
		t.Fatalf("no model and policy off must not request transcription, got %v", session["input_audio_transcription"])
	}
	policyOn := live.LiveConfig{Policies: live.LivePolicies{EnableInputAudioTranscription: true}}
	if got := object(t, roundTrip(t, off.BuildSession(policyOn, "m", "")), "input_audio_transcription")["model"]; got != voicelive.DefaultTranscriptionModel {
		t.Fatalf("policy-enabled transcription model = %v", got)
	}
}

func TestBuildSessionVoiceDefaultsAndOptions(t *testing.T) {
	t.Parallel()
	p := voicelive.New()
	voice := object(t, roundTrip(t, p.BuildSession(live.LiveConfig{}, "m", "")), "voice")
	if voice["name"] != voicelive.DefaultVoice || voice["type"] != voicelive.DefaultVoiceType {
		t.Fatalf("default voice = %v", voice)
	}
	if _, ok := voice["style"]; ok {
		t.Fatalf("voice.style must be omitted when unset, got %v", voice)
	}

	p.VoiceType = "azure-custom"
	p.VoiceRate = "+10%"
	p.NoiseSuppression = false
	p.EchoCancellation = false
	session := roundTrip(t, p.BuildSession(live.LiveConfig{Voice: "my-voice"}, "m", ""))
	voice = object(t, session, "voice")
	if voice["name"] != "my-voice" || voice["type"] != "azure-custom" || voice["rate"] != "+10%" {
		t.Fatalf("configured voice = %v", voice)
	}
	if _, ok := session["input_audio_noise_reduction"]; ok {
		t.Fatalf("noise reduction must be omitted when disabled")
	}
	if _, ok := session["input_audio_echo_cancellation"]; ok {
		t.Fatalf("echo cancellation must be omitted when disabled")
	}
}

// End to end against a fake Voice Live host: the dial carries api-version,
// model and the resource key, the session.update is the flat Voice Live
// shape, and the OpenAI event parser turns audio and response.done into
// kernel messages.
func TestConnectAndReceiveAgainstVoiceLiveServer(t *testing.T) {
	t.Parallel()
	type handshake struct {
		query  url.Values
		header http.Header
	}
	handshakes := make(chan handshake, 1)
	updates := make(chan map[string]any, 1)
	serverErrs := make(chan error, 8)
	wantAudio := []byte{0x01, 0x02, 0x03, 0x04}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakes <- handshake{query: r.URL.Query(), header: r.Header.Clone()}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			serverErrs <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		write := func(v any) {
			body, _ := json.Marshal(v)
			if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
				serverErrs <- err
			}
		}
		write(map[string]any{"type": "session.created", "session": map[string]any{"id": "sess_1", "model": "gpt-realtime-2"}})
		_, data, err := conn.Read(ctx)
		if err != nil {
			serverErrs <- err
			return
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			serverErrs <- err
			return
		}
		updates <- frame
		write(map[string]any{"type": "session.updated", "session": frame["session"]})
		write(map[string]any{"type": "response.audio.delta", "delta": base64.StdEncoding.EncodeToString(wantAudio)})
		write(map[string]any{"type": "response.done"})
		_, _, _ = conn.Read(ctx) // wait for the client to close
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := voicelive.New()
	cfg := live.LiveConfig{
		Endpoint: wsEndpoint(server.URL),
		APIKey:   "resource-key",
		Voice:    "en-US-Harper:MAI-Voice-2-Flash",
		Locale:   "en-US",
		Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{Automatic: true}},
	}
	if err := p.Connect(ctx, cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()

	hs := recv(t, handshakes)
	if hs.query.Get("api-version") != voicelive.DefaultAPIVersion || hs.query.Get("model") != voicelive.DefaultModel {
		t.Fatalf("dial query = %v", hs.query)
	}
	if hs.header.Get("api-key") != "resource-key" || hs.header.Get("Authorization") != "" {
		t.Fatalf("dial headers = %v, want api-key only", hs.header)
	}

	update := recv(t, updates)
	if update["type"] != "session.update" {
		t.Fatalf("first client frame = %v, want session.update", update["type"])
	}
	session := object(t, update, "session")
	if object(t, session, "voice")["name"] != "en-US-Harper:MAI-Voice-2-Flash" || object(t, session, "turn_detection")["type"] != "azure_semantic_vad" {
		t.Fatalf("session.update = %v", session)
	}
	if _, ok := session["model"]; ok {
		t.Fatalf("session.update must not repeat the model, got %v", session)
	}

	msg, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive audio: %v", err)
	}
	if msg.EventType != live.LiveEventOutputAudio || string(msg.Audio) != string(wantAudio) {
		t.Fatalf("first message = %+v, want output audio % x", msg, wantAudio)
	}
	msg, err = p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive turn end: %v", err)
	}
	if msg.EventType != live.LiveEventTurnEnd || !msg.Done {
		t.Fatalf("second message = %+v, want turn end", msg)
	}
	select {
	case err := <-serverErrs:
		t.Fatalf("server: %v", err)
	default:
	}
}

// A brain the resource's region does not serve is rejected with an error
// event right after connect; it must reach the host as a readable error.
func TestReceiveSurfacesUnsupportedModelError(t *testing.T) {
	t.Parallel()
	const message = "Model gpt-unknown is not supported in this region."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		body, _ := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "invalid_request_error",
				"code":    "model_not_supported",
				"message": message,
			},
		})
		_ = conn.Write(ctx, websocket.MessageText, body)
		_, _, _ = conn.Read(ctx) // session.update
		_, _, _ = conn.Read(ctx) // client close
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := voicelive.New()
	if err := p.Connect(ctx, live.LiveConfig{Endpoint: wsEndpoint(server.URL), APIKey: "k", Model: "gpt-unknown"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()

	_, err := p.Receive(ctx)
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("Receive = %v, want the server's message", err)
	}
}

func wsEndpoint(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/voice-live/realtime"
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the fake server")
		var zero T
		return zero
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// roundTrip marshals the session the way the provider does, so assertions
// see the wire types (float64 numbers, []any lists, null for nil).
func roundTrip(t *testing.T, session map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	return out
}

func object(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	obj, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return obj
}

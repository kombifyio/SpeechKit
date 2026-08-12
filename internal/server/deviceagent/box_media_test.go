package deviceagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const testBoxMediaPairingToken = "box-media-pairing-0123456789abcdef"

func TestBoxMediaFiniteTurnRunsLocalSTTExistingHAG0AndPCMReplay(t *testing.T) {
	fixture := newBoxMediaFixture(t)
	requestID := mustBoxUUIDv7(t)
	input := boxL16(16000) // one second

	first := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, requestID, input))
	if first.Code != http.StatusOK {
		t.Fatalf("first turn status=%d body=%s", first.Code, first.Body.String())
	}
	if got, want := first.Header().Get("Content-Type"), boxMediaContentType; got != want {
		t.Fatalf("content type=%q, want %q", got, want)
	}
	if got := first.Header().Get(wireServerInstanceHeaderForTest()); got != "homelab-1" {
		t.Fatalf("server identity=%q", got)
	}
	if got := first.Header().Get(BoxMediaRequestIDHeader); got != requestID {
		t.Fatalf("request identity=%q", got)
	}
	if got := first.Header().Get(BoxMediaDeviceIDHeader); got != "speaker-kitchen-001" {
		t.Fatalf("device identity=%q", got)
	}
	if got := first.Header().Get(BoxMediaPairingIDHeader); got != "pairing-kitchen-v1" {
		t.Fatalf("pairing identity=%q", got)
	}
	inputDigest := sha256.Sum256(input)
	if got, want := first.Header().Get(BoxMediaInputSHA256Header), hex.EncodeToString(inputDigest[:]); got != want {
		t.Fatalf("input digest=%q, want %q", got, want)
	}
	if got := first.Header().Get(BoxMediaReplayHeader); got != "false" {
		t.Fatalf("first replay header=%q", got)
	}
	if got, want := len(first.Body.Bytes()), 4800*2; got != want {
		t.Fatalf("48 kHz L16 length=%d, want %d", got, want)
	}
	outputDigest := sha256.Sum256(first.Body.Bytes())
	if got, want := first.Header().Get(BoxMediaOutputSHA256Header), hex.EncodeToString(outputDigest[:]); got != want {
		t.Fatalf("output digest=%q, want %q", got, want)
	}
	if got := int16(binary.BigEndian.Uint16(first.Body.Bytes()[:2])); got != 900 {
		t.Fatalf("first L16 network-order sample=%d, want 900", got)
	}
	if fixture.stt.calls != 1 || !bytes.HasPrefix(fixture.stt.lastAudio, []byte("RIFF")) || string(fixture.stt.lastAudio[8:12]) != "WAVE" {
		t.Fatalf("local STT calls=%d input_prefix=%q", fixture.stt.calls, fixture.stt.lastAudio[:min(len(fixture.stt.lastAudio), 12)])
	}
	if len(fixture.stt.lastAudio) < 44 || !bytes.Equal(fixture.stt.lastAudio[44:], l16ToPCM16LE(input)) {
		t.Fatal("audio/L16 network-byte-order input was not converted exactly to host PCM16LE for STT")
	}
	if fixture.stt.locale != "en" {
		t.Fatalf("local STT locale=%q", fixture.stt.locale)
	}
	if fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 || fixture.ha.verifyEntity != "light.kitchen" || fixture.ha.verifyState != "off" {
		t.Fatalf("HA calls converse=%d verify=%d entity=%q state=%q", fixture.ha.converseCalls, fixture.ha.verifyCalls, fixture.ha.verifyEntity, fixture.ha.verifyState)
	}

	replay := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, requestID, input))
	if replay.Code != http.StatusOK || replay.Header().Get(BoxMediaReplayHeader) != "true" {
		t.Fatalf("replay status=%d replay=%q body=%s", replay.Code, replay.Header().Get(BoxMediaReplayHeader), replay.Body.String())
	}
	if !bytes.Equal(replay.Body.Bytes(), first.Body.Bytes()) {
		t.Fatal("completed claim replay changed the rendered L16 response")
	}
	if fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 {
		t.Fatalf("replay repeated HA side effects: converse=%d verify=%d", fixture.ha.converseCalls, fixture.ha.verifyCalls)
	}

	differentInput := append([]byte(nil), input...)
	differentInput[0] ^= 0x01
	conflict := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, requestID, differentInput))
	assertBridgeError(t, conflict, http.StatusConflict, "request_digest_mismatch", "no")
	if fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 {
		t.Fatalf("audio conflict repeated HA side effects: converse=%d verify=%d", fixture.ha.converseCalls, fixture.ha.verifyCalls)
	}
}

func TestBoxMediaRejectsUnsafeTransportIdentityAndAudioBeforeHA(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*http.Request)
		status     int
		reason     string
		sttAllowed bool
	}{
		{
			name: "plaintext", status: http.StatusUpgradeRequired, reason: "box_media_tls13_required",
			mutate: func(request *http.Request) { request.TLS = nil },
		},
		{
			name: "TLS 1.2", status: http.StatusUpgradeRequired, reason: "box_media_tls13_required",
			mutate: func(request *http.Request) { request.TLS.Version = tls.VersionTLS12 },
		},
		{
			name: "path suffix does not redirect", status: http.StatusNotFound, reason: "box_media_route_unknown",
			mutate: func(request *http.Request) { request.URL.Path = BoxMediaTurnPath + "/" },
		},
		{
			name: "G0 route is not exposed on media listener", status: http.StatusNotFound, reason: "box_media_route_unknown",
			mutate: func(request *http.Request) { request.URL.Path = "/v1/device-agent/assist" },
		},
		{
			name: "GET", status: http.StatusMethodNotAllowed, reason: "box_media_post_required",
			mutate: func(request *http.Request) { request.Method = http.MethodGet },
		},
		{
			name: "chunked", status: http.StatusLengthRequired, reason: "finite_content_length_required",
			mutate: func(request *http.Request) {
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
			},
		},
		{
			name: "encoded", status: http.StatusUnsupportedMediaType, reason: "content_encoding_forbidden",
			mutate: func(request *http.Request) { request.Header.Set("Content-Encoding", "gzip") },
		},
		{
			name: "wrong media type", status: http.StatusUnsupportedMediaType, reason: "l16_16khz_mono_required",
			mutate: func(request *http.Request) { request.Header.Set("Content-Type", "audio/L16; rate=48000; channels=1") },
		},
		{
			name: "wrong token", status: http.StatusUnauthorized, reason: "pairing_credential_invalid",
			mutate: func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32)) },
		},
		{
			name: "G0 route token cannot authenticate media", status: http.StatusUnauthorized, reason: "pairing_credential_invalid",
			mutate: func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+testPairingToken) },
		},
		{
			name: "oversized token", status: http.StatusUnauthorized, reason: "pairing_credential_invalid",
			mutate: func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 513)) },
		},
		{
			name: "pairing mismatch", status: http.StatusForbidden, reason: "pairing_id_mismatch",
			mutate: func(request *http.Request) { request.Header.Set(BoxMediaPairingIDHeader, "pairing-other-v1") },
		},
		{
			name: "forwarded local address cannot rescue public peer", status: http.StatusForbidden, reason: "source_cidr_not_allowed",
			mutate: func(request *http.Request) {
				request.RemoteAddr = "203.0.113.9:443"
				request.Header.Set("Forwarded", "for=127.0.0.1")
				request.Header.Set("X-Forwarded-For", "127.0.0.1")
				request.Header.Set("X-Real-IP", "127.0.0.1")
			},
		},
		{
			name: "missing request id", status: http.StatusUnprocessableEntity, reason: "request_or_audio_identity_invalid",
			mutate: func(request *http.Request) { request.Header.Del(BoxMediaRequestIDHeader) },
		},
		{
			name: "uppercase digest", status: http.StatusUnprocessableEntity, reason: "request_or_audio_identity_invalid",
			mutate: func(request *http.Request) {
				request.Header.Set(BoxMediaInputSHA256Header, strings.ToUpper(request.Header.Get(BoxMediaInputSHA256Header)))
			},
		},
		{
			name: "digest mismatch", status: http.StatusUnprocessableEntity, reason: "audio_sha256_mismatch",
			mutate: func(request *http.Request) { request.Header.Set(BoxMediaInputSHA256Header, strings.Repeat("0", 64)) },
		},
		{
			name: "non-UUIDv7", status: http.StatusUnprocessableEntity, reason: "request_id_not_uuidv7",
			mutate: func(request *http.Request) { request.Header.Set(BoxMediaRequestIDHeader, uuid.NewString()) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBoxMediaFixture(t)
			request := boxMediaRequest(t, mustBoxUUIDv7(t), boxL16(16000))
			test.mutate(request)
			response := serveBoxMedia(t, fixture.handler, request)
			assertBridgeError(t, response, test.status, test.reason, "no")
			if !test.sttAllowed && fixture.stt.calls != 0 {
				t.Fatalf("rejected request reached STT %d times", fixture.stt.calls)
			}
			if fixture.ha.converseCalls != 0 || fixture.ha.verifyCalls != 0 {
				t.Fatalf("rejected request reached HA: converse=%d verify=%d", fixture.ha.converseCalls, fixture.ha.verifyCalls)
			}
			if strings.Contains(response.Body.String(), testPairingToken) || strings.Contains(response.Body.String(), fixture.stt.text) {
				t.Fatalf("error response leaked token or transcript: %s", response.Body.String())
			}
		})
	}
}

func TestBridgeMountDoesNotExposeBoxMediaRoute(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	mux := http.NewServeMux()
	bridge.Mount(mux)
	request := boxMediaRequest(t, mustBoxUUIDv7(t), boxL16(8000))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("Bridge.Mount exposed Box media route: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBoxMediaEnforcesDurationAndExactTranscript(t *testing.T) {
	t.Run("shorter than 300ms", func(t *testing.T) {
		fixture := newBoxMediaFixture(t)
		response := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, mustBoxUUIDv7(t), make([]byte, minBoxMediaInputBytes()-2)))
		assertBridgeError(t, response, http.StatusRequestEntityTooLarge, "audio_must_be_300ms_to_15s", "no")
		if fixture.stt.calls != 0 || fixture.ha.converseCalls != 0 {
			t.Fatal("short audio reached STT or HA")
		}
	})

	t.Run("longer than 15s", func(t *testing.T) {
		fixture := newBoxMediaFixture(t)
		response := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, mustBoxUUIDv7(t), make([]byte, maxBoxMediaInputBytes()+2)))
		assertBridgeError(t, response, http.StatusRequestEntityTooLarge, "audio_must_be_300ms_to_15s", "no")
		if fixture.stt.calls != 0 || fixture.ha.converseCalls != 0 {
			t.Fatal("long audio reached STT or HA")
		}
	})

	t.Run("exact transcript only", func(t *testing.T) {
		fixture := newBoxMediaFixture(t)
		fixture.stt.text = "please switch off the kitchen light"
		response := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, mustBoxUUIDv7(t), boxL16(8000)))
		assertBridgeError(t, response, http.StatusForbidden, "box_media_transcript_mismatch", "no")
		if fixture.ha.converseCalls != 0 || fixture.ha.verifyCalls != 0 {
			t.Fatal("unmapped transcript reached HA")
		}
	})

	t.Run("oversized local transcript", func(t *testing.T) {
		fixture := newBoxMediaFixture(t)
		fixture.stt.text = strings.Repeat("x", maxPolicyTriggerTextBytes+1)
		response := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, mustBoxUUIDv7(t), boxL16(8000)))
		assertBridgeError(t, response, http.StatusForbidden, "box_media_transcript_mismatch", "no")
		if fixture.ha.converseCalls != 0 || fixture.ha.verifyCalls != 0 {
			t.Fatal("oversized transcript reached HA")
		}
	})

	t.Run("local STT failure has no fallback", func(t *testing.T) {
		fixture := newBoxMediaFixture(t)
		fixture.stt.err = errors.New("local provider offline")
		response := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, mustBoxUUIDv7(t), boxL16(8000)))
		assertBridgeError(t, response, http.StatusServiceUnavailable, "local_stt_failed", "no")
		if fixture.ha.converseCalls != 0 || fixture.ha.verifyCalls != 0 {
			t.Fatal("local STT failure fell through to HA")
		}
	})
}

func TestBoxMediaTTSRetryUsesCompletedHAClaim(t *testing.T) {
	fixture := newBoxMediaFixture(t)
	fixture.tts.invalid = true
	requestID := mustBoxUUIDv7(t)
	requestAudio := boxL16(8000)
	first := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, requestID, requestAudio))
	assertBridgeError(t, first, http.StatusBadGateway, "tts_pcm_contract_invalid", "not_applicable")
	if fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 {
		t.Fatalf("first TTS failure HA calls converse=%d verify=%d", fixture.ha.converseCalls, fixture.ha.verifyCalls)
	}

	fixture.tts.invalid = false
	retry := serveBoxMedia(t, fixture.handler, boxMediaRequest(t, requestID, requestAudio))
	if retry.Code != http.StatusOK || retry.Header().Get(BoxMediaReplayHeader) != "true" {
		t.Fatalf("TTS retry status=%d replay=%q body=%s", retry.Code, retry.Header().Get(BoxMediaReplayHeader), retry.Body.String())
	}
	if fixture.ha.converseCalls != 1 || fixture.ha.verifyCalls != 1 {
		t.Fatalf("TTS retry repeated HA: converse=%d verify=%d", fixture.ha.converseCalls, fixture.ha.verifyCalls)
	}
}

func TestNewBoxMediaHandlerRequiresLocalSTTAndMatchingSingleRule(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	validRule := boxMediaTestRule()

	if _, err := NewBoxMediaHandler(BoxMediaHandlerOptions{Bridge: &Bridge{}, LocalSTT: &fakeBoxSTT{local: true}, MediaPairingToken: testBoxMediaPairingToken, Rule: validRule}); !errors.Is(err, ErrBoxMediaBridgeRequired) {
		t.Fatalf("uninitialized bridge error=%v, want ErrBoxMediaBridgeRequired", err)
	}
	if _, err := NewBoxMediaHandler(BoxMediaHandlerOptions{Bridge: bridge, LocalSTT: &fakeBoxSTT{local: false}, MediaPairingToken: testBoxMediaPairingToken, Rule: validRule}); !errors.Is(err, ErrBoxMediaLocalSTTRequired) {
		t.Fatalf("non-local STT error=%v", err)
	}
	for name, token := range map[string]string{
		"missing":           "",
		"short":             "too-short",
		"oversized":         strings.Repeat("x", 513),
		"invalid character": strings.Repeat("x", 31) + "!",
		"same as G0 route":  testPairingToken,
	} {
		t.Run(name+" media token", func(t *testing.T) {
			if _, err := NewBoxMediaHandler(BoxMediaHandlerOptions{Bridge: bridge, LocalSTT: &fakeBoxSTT{local: true}, MediaPairingToken: token, Rule: validRule}); !errors.Is(err, ErrBoxMediaTokenInvalid) {
				t.Fatalf("NewBoxMediaHandler error=%v, want ErrBoxMediaTokenInvalid", err)
			}
		})
	}
	mutations := []struct {
		name   string
		mutate func(*BoxMediaRuleOptions)
	}{
		{name: "unknown device", mutate: func(rule *BoxMediaRuleOptions) { rule.DeviceID = "speaker-other-001" }},
		{name: "wrong pairing", mutate: func(rule *BoxMediaRuleOptions) { rule.PairingID = "pairing-other-v1" }},
		{name: "wrong room", mutate: func(rule *BoxMediaRuleOptions) { rule.RoomID = "office" }},
		{name: "unknown command", mutate: func(rule *BoxMediaRuleOptions) { rule.CommandID = "other-command" }},
		{name: "transcript differs from G0", mutate: func(rule *BoxMediaRuleOptions) { rule.Transcript = "turn on the kitchen light" }},
		{name: "locale differs from G0", mutate: func(rule *BoxMediaRuleOptions) { rule.Locale = "de-DE" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			rule := validRule
			test.mutate(&rule)
			if _, err := NewBoxMediaHandler(BoxMediaHandlerOptions{Bridge: bridge, LocalSTT: &fakeBoxSTT{local: true}, MediaPairingToken: testBoxMediaPairingToken, Rule: rule}); !errors.Is(err, ErrBoxMediaRuleInvalid) {
				t.Fatalf("NewBoxMediaHandler error=%v, want ErrBoxMediaRuleInvalid", err)
			}
		})
	}
}

func TestNewBoxMediaHandlerRejectsSelectedBindingWithoutRFC1918Source(t *testing.T) {
	for name, cidr := range map[string]string{
		"loopback":      "127.0.0.1/32",
		"link-local":    "169.254.10.0/24",
		"CGNAT":         "100.64.10.0/24",
		"IPv6 loopback": "::1/128",
		"IPv6 ULA":      "fd00::/64",
	} {
		t.Run(name, func(t *testing.T) {
			bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
			binding, err := newDeviceBinding(DeviceBindingOptions{
				PairingID: "pairing-kitchen-v1", DeviceID: "speaker-kitchen-001", RoomID: "kitchen",
				Token: testPairingToken, AllowedClientCIDRs: []string{cidr},
			})
			if err != nil {
				t.Fatalf("newDeviceBinding: %v", err)
			}
			bridge.bindings[binding.deviceID] = binding
			_, err = NewBoxMediaHandler(BoxMediaHandlerOptions{
				Bridge: bridge, LocalSTT: &fakeBoxSTT{local: true},
				MediaPairingToken: testBoxMediaPairingToken, Rule: boxMediaTestRule(),
			})
			if !errors.Is(err, ErrBoxMediaRuleInvalid) {
				t.Fatalf("NewBoxMediaHandler error=%v, want ErrBoxMediaRuleInvalid", err)
			}
		})
	}
}

func TestNewBoxMediaHandlerRejectsEveryG0PairingCredential(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	otherToken := "other-device-pairing-0123456789abcdef"
	otherBinding, err := newDeviceBinding(DeviceBindingOptions{
		PairingID: "pairing-office-v1", DeviceID: "speaker-office-001", RoomID: "office",
		Token: otherToken, AllowedClientCIDRs: []string{"127.0.0.1/32"},
	})
	if err != nil {
		t.Fatalf("new other device binding: %v", err)
	}
	bridge.bindings[otherBinding.deviceID] = otherBinding

	_, err = NewBoxMediaHandler(BoxMediaHandlerOptions{
		Bridge: bridge, LocalSTT: &fakeBoxSTT{local: true},
		MediaPairingToken: otherToken, Rule: boxMediaTestRule(),
	})
	if !errors.Is(err, ErrBoxMediaTokenInvalid) {
		t.Fatalf("NewBoxMediaHandler error=%v, want ErrBoxMediaTokenInvalid for another device's G0 token", err)
	}
}

type boxMediaFixture struct {
	handler *BoxMediaHandler
	ha      *fakeHA
	stt     *fakeBoxSTT
	tts     *boxMediaTTS
}

func newBoxMediaFixture(t *testing.T) boxMediaFixture {
	t.Helper()
	ha := &fakeHA{result: &HomeAssistantResult{
		ConversationID: "ha-box-1", ResponseType: "action_done", Speech: "The kitchen light is off.",
		SuccessTargets: []HomeAssistantTarget{{Type: "entity", ID: "light.kitchen"}}, ActionExecuted: "unknown",
	}}
	bridge := newTestBridge(t, ha, newFakeLedger())
	ttsProvider := &boxMediaTTS{}
	bridge.tts = ttsProvider
	sttProvider := &fakeBoxSTT{local: true, text: "turn off the kitchen light", provider: "local-whisper"}
	handler, err := NewBoxMediaHandler(BoxMediaHandlerOptions{Bridge: bridge, LocalSTT: sttProvider, MediaPairingToken: testBoxMediaPairingToken, Rule: boxMediaTestRule()})
	if err != nil {
		t.Fatalf("NewBoxMediaHandler: %v", err)
	}
	return boxMediaFixture{handler: handler, ha: ha, stt: sttProvider, tts: ttsProvider}
}

func boxMediaTestRule() BoxMediaRuleOptions {
	return BoxMediaRuleOptions{
		DeviceID: "speaker-kitchen-001", PairingID: "pairing-kitchen-v1", RoomID: "kitchen",
		Transcript: "turn off the kitchen light", CommandID: "kitchen-light-off-en", Locale: "en",
	}
}

type fakeBoxSTT struct {
	local     bool
	text      string
	provider  string
	err       error
	calls     int
	lastAudio []byte
	locale    string
}

func (f *fakeBoxSTT) LocalOnly() bool { return f != nil && f.local }

func (f *fakeBoxSTT) TranscribeLocal(_ context.Context, wav []byte, locale string) (*BoxMediaTranscript, error) {
	f.calls++
	f.lastAudio = append([]byte(nil), wav...)
	f.locale = locale
	if f.err != nil {
		return nil, f.err
	}
	return &BoxMediaTranscript{Text: f.text, Provider: f.provider}, nil
}

type boxMediaTTS struct {
	calls   int
	invalid bool
}

func (t *boxMediaTTS) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	t.calls++
	if t.invalid {
		return &tts.Result{Audio: []byte("invalid wav"), Format: "wav", SampleRate: 16000, Provider: "fake-local-piper"}, nil
	}
	pcm := boxPCM16LE(1600) // 100ms at 16 kHz -> 4800 samples at 48 kHz
	return &tts.Result{Audio: boxTestWAV(pcm, 16000), Format: "wav", SampleRate: 16000, Provider: "fake-local-piper"}, nil
}

func (*boxMediaTTS) ReadyHealthCheck(context.Context) map[string]error {
	return map[string]error{"fake-local-piper": nil}
}

func boxMediaRequest(t *testing.T, requestID string, pcm []byte) *http.Request {
	t.Helper()
	digest := sha256.Sum256(pcm)
	request := httptest.NewRequest(http.MethodPost, "https://speechkit.local"+BoxMediaTurnPath, bytes.NewReader(pcm))
	request.RemoteAddr = "127.0.0.1:49321"
	request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	request.Header.Set("Content-Type", "audio/L16; rate=16000; channels=1")
	request.Header.Set("Authorization", "Bearer "+testBoxMediaPairingToken)
	request.Header.Set(BoxMediaDeviceIDHeader, "speaker-kitchen-001")
	request.Header.Set(BoxMediaPairingIDHeader, "pairing-kitchen-v1")
	request.Header.Set(BoxMediaRequestIDHeader, requestID)
	request.Header.Set(BoxMediaInputSHA256Header, hex.EncodeToString(digest[:]))
	return request
}

func serveBoxMedia(t *testing.T, handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mustBoxUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid v7: %v", err)
	}
	return id.String()
}

func boxPCM16LE(samples int) []byte {
	pcm := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		sample := int16(900)
		if (index/80)%2 == 1 {
			sample = -sample
		}
		binary.LittleEndian.PutUint16(pcm[index*2:], uint16(sample)) // #nosec G115 -- identical-width PCM representation.
	}
	return pcm
}

func boxL16(samples int) []byte {
	pcm := boxPCM16LE(samples)
	l16 := make([]byte, len(pcm))
	for index := 0; index < len(pcm); index += 2 {
		binary.BigEndian.PutUint16(l16[index:], binary.LittleEndian.Uint16(pcm[index:]))
	}
	return l16
}

func boxTestWAV(pcm []byte, rate int) []byte {
	out := make([]byte, 44+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm))) // #nosec G115 -- test fixture is bounded.
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], uint32(rate))   // #nosec G115 -- test rate is positive and bounded.
	binary.LittleEndian.PutUint32(out[28:32], uint32(rate*2)) // #nosec G115 -- test rate is positive and bounded.
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm))) // #nosec G115 -- test fixture is bounded.
	copy(out[44:], pcm)
	return out
}

func wireServerInstanceHeaderForTest() string { return "X-SpeechKit-Server-Instance-ID" }

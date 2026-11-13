package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/homegraph"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/x/deviceagent"
)

func TestBridgeAuthBindingCIDRAndStrictJSONFailClosed(t *testing.T) {
	bridge := newTestBridge(t, &fakeHA{}, newFakeLedger())
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := wire.Registration{Version: wire.CurrentProtocolVersion, Device: wire.DeviceDescriptor{DeviceID: "speaker-kitchen-001", RoomID: "kitchen"}}
	wrongToken := postBridge(t, server.URL+"/v1/device-agent/register", body, "general-server-token", "speaker-kitchen-001")
	assertBridgeError(t, wrongToken, http.StatusUnauthorized, "pairing_credential_invalid", "no")

	body.Device.RoomID = "office"
	wrongRoom := postBridge(t, server.URL+"/v1/device-agent/register", body, testPairingToken, "speaker-kitchen-001")
	assertBridgeError(t, wrongRoom, http.StatusForbidden, "room_id_mismatch", "no")

	request := httptest.NewRequest(http.MethodPost, "/v1/device-agent/register", bytes.NewBufferString(`{"version":"speechkit.device_agent.v1","device":{"device_id":"speaker-kitchen-001","room_id":"kitchen"}}`))
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("Authorization", "Bearer "+testPairingToken)
	request.Header.Set("X-SpeechKit-Device-ID", "speaker-kitchen-001")
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	blocked := httptest.NewRecorder()
	mux.ServeHTTP(blocked, request)
	assertBridgeError(t, blocked, http.StatusForbidden, "source_cidr_not_allowed", "no")

	unknownField := httptest.NewRequest(http.MethodPost, "/v1/device-agent/assist", bytes.NewBufferString(`{"request_id":"x","session_id":"s","device_id":"speaker-kitchen-001","room_id":"kitchen","text":"x","locale":"en","home_assistant_url":"http://evil"}`))
	unknownField.RemoteAddr = "127.0.0.1:4567"
	unknownField.Header.Set("Authorization", "Bearer "+testPairingToken)
	unknownField.Header.Set("X-SpeechKit-Device-ID", "speaker-kitchen-001")
	strict := httptest.NewRecorder()
	mux.ServeHTTP(strict, unknownField)
	assertBridgeError(t, strict, http.StatusBadRequest, "request_body_invalid", "no")

	for _, response := range []*httptest.ResponseRecorder{wrongToken, wrongRoom, blocked, strict} {
		if got := response.Header().Get(wire.ServerInstanceHeader); got != "homelab-1" {
			t.Fatalf("server identity header = %q", got)
		}
	}
}

func TestBridgeRejectsInvalidUUIDv7WindowBeforeClaimOrHA(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{}
	now := time.Now().UTC()
	bridge := newTestBridgeWithNow(t, ha, ledger, func() time.Time { return now })
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	for _, requestID := range []string{"not-a-uuid", uuid.New().String()} {
		request := wire.AssistRequest{
			RequestID: requestID, SessionID: "session-1", DeviceID: "speaker-kitchen-001",
			CommandID: "kitchen-light-off-en", RoomID: "kitchen", Text: "turn off the kitchen light", Locale: "en",
		}
		response := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
		assertBridgeError(t, response, http.StatusUnprocessableEntity, "request_id_not_uuidv7", "no")
	}
	if ledger.claimCalls != 0 || ha.converseCalls != 0 {
		t.Fatalf("claim calls=%d HA calls=%d", ledger.claimCalls, ha.converseCalls)
	}
}

func TestBridgeLocalPolicyDeniesBeforeClaimOrHomeAssistant(t *testing.T) {
	ledger := newFakeLedger()
	ha := &fakeHA{}
	bridge := newTestBridge(t, ha, ledger)
	mux := http.NewServeMux()
	bridge.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	id, _ := uuid.NewV7()
	request := wire.AssistRequest{
		RequestID: id.String(), SessionID: "session-1", CommandID: "not-authorized",
		DeviceID: "speaker-kitchen-001", RoomID: "kitchen", Text: "unlock the front door", Locale: "en",
	}
	response := postBridge(t, server.URL+"/v1/device-agent/assist", request, testPairingToken, request.DeviceID)
	assertBridgeError(t, response, http.StatusForbidden, PolicyReasonRuleNotFound, "no")
	if ledger.claimCalls != 0 || ha.converseCalls != 0 {
		t.Fatalf("denied policy request reached claim/HA: claim=%d HA=%d", ledger.claimCalls, ha.converseCalls)
	}
}

func TestNewBridgeRejectsUnsafeOrAliasedBindings(t *testing.T) {
	now := time.Now().UTC()
	policy, err := NewPolicy(RuleOptions{
		RuleID: "light-off", DeviceID: "device-one", RoomID: "room-one", TriggerText: "light off",
		Locale: "en", Action: ActionTurnOff, EntityID: "light.one", NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	base := BridgeOptions{
		ServerInstanceID: "server-one", HomeAssistant: &fakeHA{}, TTS: fakeTTS{}, TTSReady: true,
		Claims: newFakeLedger(), Policy: policy, MaxRequestAge: time.Minute,
		Bindings: []DeviceBindingOptions{{
			PairingID: "pair-one", DeviceID: "device-one", RoomID: "room-one",
			Token: testPairingToken, AllowedClientCIDRs: []string{"127.0.0.1/32"},
		}},
	}
	tests := []struct {
		name   string
		mutate func(*BridgeOptions)
	}{
		{name: "public CIDR", mutate: func(opts *BridgeOptions) { opts.Bindings[0].AllowedClientCIDRs = []string{"0.0.0.0/0"} }},
		{name: "header unsafe server id", mutate: func(opts *BridgeOptions) { opts.ServerInstanceID = "server\r\nevil" }},
		{name: "duplicate pairing", mutate: func(opts *BridgeOptions) {
			opts.Bindings = append(opts.Bindings, DeviceBindingOptions{PairingID: "pair-one", DeviceID: "device-two", RoomID: "room-two", Token: "second-pairing-token-0123456789abc", AllowedClientCIDRs: []string{"127.0.0.1/32"}})
		}},
		{name: "duplicate token", mutate: func(opts *BridgeOptions) {
			opts.Bindings = append(opts.Bindings, DeviceBindingOptions{PairingID: "pair-two", DeviceID: "device-two", RoomID: "room-two", Token: testPairingToken, AllowedClientCIDRs: []string{"127.0.0.1/32"}})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			opts.Bindings = append([]DeviceBindingOptions(nil), base.Bindings...)
			tc.mutate(&opts)
			if _, err := NewBridge(opts); err == nil {
				t.Fatal("NewBridge accepted unsafe binding")
			}
		})
	}
}

// The graph is an output authorization boundary: spoken input never supplies
// its room or target. Wire examples follow kombify.home_graph.v1.
func TestHomeGraphAnswerAuthority(t *testing.T) {
	const space = "spc_000000000000000000000001"
	const room = "rom_000000000000000000000001"
	const source = "hdv_000000000000000000000001"
	const speaker = "hdv_000000000000000000000002"
	document := `{"schema":"kombify.home_graph.v1","space_id":"` + space + `","revision":1,"rooms":[{"id":"` + room + `"}],"devices":[
 {"id":"` + source + `","class":"voice_device","status":"active","roles":["audio_in","audio_out"],"room_id":"` + room + `","identity":{"kind":"edge_device","ref":"box-1"}},
 {"id":"` + speaker + `","class":"speaker","status":"active","roles":["audio_out","announce"],"room_id":"` + room + `","identity":{"kind":"external","ref":"sonos-device"},"external_refs":[{"system":"home_assistant","ref":"sonos-device"}]}],"assignments":[]}`
	graph, err := homegraph.Read(strings.NewReader(document), space)
	if err != nil {
		t.Fatal(err)
	}
	route, err := graph.Resolve("edge_device", "box-1")
	if err != nil || route.DeviceID != speaker || route.Local || !route.Announce || route.Reason != "room_announce" {
		t.Fatalf("room answer: %+v, %v", route, err)
	}
	for _, tc := range []struct{ name, kind, ref string }{
		{"speaker microphone", "external", "sonos-device"},
		{"unknown device", "edge_device", "unknown"},
		{"forged registry", "connect_installation", "box-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := graph.Resolve(tc.kind, tc.ref); err == nil {
				t.Fatal("unpaired input selected a speaker")
			}
		})
	}
	if _, err := homegraph.Read(strings.NewReader(document), "spc_000000000000000000000002"); err == nil {
		t.Fatal("foreign household admitted")
	}
	explicit := strings.Replace(document, `"assignments":[]`, `"assignments":[{"kind":"answers_on","device_id":"`+source+`","target_device_id":"`+source+`"}]`, 1)
	graph, err = homegraph.Read(strings.NewReader(explicit), space)
	if err != nil {
		t.Fatal(err)
	}
	route, err = graph.Resolve("edge_device", "box-1")
	if err != nil || !route.Local || route.Reason != "answers_on" {
		t.Fatalf("explicit answer: %+v, %v", route, err)
	}
	revoked := strings.Replace(document, `"class":"voice_device","status":"active"`, `"class":"voice_device","status":"revoked"`, 1)
	graph, err = homegraph.Read(strings.NewReader(revoked), space)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Resolve("edge_device", "box-1"); err == nil {
		t.Fatal("revoked input selected a speaker")
	}
	muted := strings.Replace(document, `["audio_in","audio_out"]`, `["audio_in"]`, 1)
	muted = strings.Replace(muted, `["audio_out","announce"]`, `["audio_out"]`, 1)
	graph, err = homegraph.Read(strings.NewReader(muted), space)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Resolve("edge_device", "box-1"); err == nil {
		t.Fatal("no output role still selected a speaker")
	}
}

func TestHomeAudioReplayNeverDispatches(t *testing.T) {
	output := &homeAudio{} // No HA client: replay must return before any I/O.
	result := &TurnResult{}
	result.Assist.Replayed = true
	if err := output.play(context.Background(), homegraph.Route{}, result); err == nil || err.Error() != "home_audio_replay_suppressed" {
		t.Fatalf("replay did not report suppression: %v", err)
	}
}

func TestHomeAudioLanguageCannotSilentlyChangeLanguage(t *testing.T) {
	for _, tc := range []struct {
		input     string
		supported []string
		want      string
	}{
		{"de-DE", []string{"en_US", "de_DE"}, "de_DE"},
		{"ar", []string{"en_US", "ar_JO"}, "ar_JO"},
		{"hi-IN", []string{"hi", "en_US"}, "hi"},
	} {
		got, err := homeAudioLanguage(tc.input, tc.supported)
		if err != nil || got != tc.want {
			t.Fatalf("language %s: %s, %v", tc.input, got, err)
		}
	}
	if _, err := homeAudioLanguage("de-DE", []string{"en_US"}); err == nil {
		t.Fatal("unsupported German answer changed to English")
	}
	if _, err := homeAudioLanguage("zh", []string{"zh_Hans", "zh_Hant"}); err == nil {
		t.Fatal("ambiguous Chinese script guessed")
	}
}

// HA response shapes come from core's entity registry, tts/engine/get and
// media_source/http.py; these assertions protect output/capability scope.
func TestHomeAudioHAOutputAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
		features       int
		want           string
	}{
		{"announcing local TTS", "wyoming", 1048576, ""},
		{"cloud TTS denied", "google_cloud", 1048576, "home_audio_output_unavailable"},
		{"unsupported announcement denied", "wyoming", 0, "home_audio_announce_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dispatches atomic.Int32
			wav := audio.PCMToWAV(make([]byte, 3200))
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/websocket":
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.CloseNow()
					ctx := r.Context()
					if wsjson.Write(ctx, conn, map[string]any{"type": "auth_required"}) != nil {
						return
					}
					var auth map[string]any
					if wsjson.Read(ctx, conn, &auth) != nil {
						return
					}
					if auth["type"] != "auth" || auth["access_token"] != "fixture-HA-token" {
						t.Error("HA socket did not authenticate")
						return
					}
					if wsjson.Write(ctx, conn, map[string]any{"type": "auth_ok"}) != nil {
						return
					}
					for {
						var command map[string]any
						if wsjson.Read(ctx, conn, &command) != nil {
							return
						}
						var result any
						switch command["type"] {
						case "config/entity_registry/list":
							result = []map[string]any{{"entity_id": "media_player.living_room", "device_id": "sonos-device", "platform": "sonos", "disabled_by": nil}, {"entity_id": "tts.piper", "platform": tc.platform, "disabled_by": nil}}
						case "tts/engine/get":
							result = map[string]any{"engine_id": "tts.piper", "supported_languages": []string{"de_DE"}}
						case "media_source/resolve_media":
							if _, exists := command["target_media_player"]; exists {
								t.Error("unsupported HA resolve field sent")
							}
							raw, _ := command["media_content_id"].(string)
							media, err := url.Parse(raw)
							if err != nil || media.Query().Get("language") != "de_DE" || media.Query().Get("message") != "Erledigt." {
								t.Error("claim language or speech lost")
							}
							result = map[string]any{"url": "/api/tts_proxy/answer.wav?authSig=fixture", "mime_type": "audio/wav"}
						default:
							t.Errorf("unexpected HA command %v", command["type"])
							return
						}
						if wsjson.Write(ctx, conn, map[string]any{"id": command["id"], "type": "result", "success": true, "result": result}) != nil {
							return
						}
					}
				case "/api/states/media_player.living_room":
					_ = json.NewEncoder(w).Encode(map[string]any{"state": "idle", "attributes": map[string]any{"supported_features": tc.features}})
				case "/api/tts_proxy/answer.wav":
					if r.URL.Query().Get("authSig") != "fixture" {
						t.Error("HA media signature lost")
					}
					_, _ = w.Write(wav)
				case "/api/services/media_player/play_media":
					var payload map[string]any
					if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["entity_id"] != "media_player.living_room" || payload["announce"] != true {
						t.Error("speaker scope or announcement changed")
					}
					dispatches.Add(1)
					_, _ = w.Write([]byte("[]"))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			origin, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := &HomeAssistantClient{baseURL: origin, token: "fixture-HA-token", client: server.Client()}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			route := homegraph.Route{DeviceID: "speaker", HADeviceID: "sonos-device", Announce: true}
			player, media, duration, err := client.prepareHomeAudio(ctx, route, "tts.piper", "Erledigt.", "de-DE", false)
			if tc.want != "" {
				if err == nil || err.Error() != tc.want {
					t.Fatalf("expected %s, got %v", tc.want, err)
				}
				if dispatches.Load() != 0 {
					t.Fatal("unauthorized output dispatched")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if duration != 100*time.Millisecond {
				t.Fatalf("wrong acoustic lease: %s", duration)
			}
			if err := client.playHomeAudio(ctx, player, media, route.Announce); err != nil {
				t.Fatal(err)
			}
			if dispatches.Load() != 1 {
				t.Fatal("authorized answer not dispatched once")
			}
		})
	}
}

package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/hajimehoshi/go-mp3"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/homegraph"
)

type homeAudioEntity struct {
	EntityID   string  `json:"entity_id"`
	DeviceID   string  `json:"device_id"`
	Platform   string  `json:"platform"`
	DisabledBy *string `json:"disabled_by"`
}

// prepareHomeAudio uses the HA registry to bind a physical graph device to its
// media_player. A graph reference is never interpreted as a service command.
func (c *HomeAssistantClient) prepareHomeAudio(ctx context.Context, route homegraph.Route, ttsEntity, speech, language string, allowInterrupt bool) (string, string, time.Duration, error) {
	fail := func() (string, string, time.Duration, error) {
		return "", "", 0, errors.New("home_audio_output_unavailable")
	}
	if !strings.HasPrefix(ttsEntity, "tts.") || len(speech) == 0 || len(speech) > maxAssistTextBytes || !validHALanguage(language) {
		return fail()
	}
	wsURL := *c.baseURL
	wsURL.Path, wsURL.Scheme = "/api/websocket", "wss"
	conn, _, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{HTTPClient: c.client})
	if err != nil {
		return fail()
	}
	defer conn.CloseNow()
	conn.SetReadLimit(2 << 20)
	var auth struct {
		Type string `json:"type"`
	}
	if wsjson.Read(ctx, conn, &auth) != nil || auth.Type != "auth_required" {
		return fail()
	}
	if wsjson.Write(ctx, conn, map[string]string{"type": "auth", "access_token": c.token}) != nil {
		return fail()
	}
	if wsjson.Read(ctx, conn, &auth) != nil || auth.Type != "auth_ok" {
		return fail()
	}
	if wsjson.Write(ctx, conn, map[string]any{"id": 1, "type": "config/entity_registry/list"}) != nil {
		return fail()
	}
	var registry struct {
		ID      int               `json:"id"`
		Success bool              `json:"success"`
		Result  []homeAudioEntity `json:"result"`
	}
	if wsjson.Read(ctx, conn, &registry) != nil || registry.ID != 1 || !registry.Success {
		return fail()
	}
	player := ""
	localTTS := false
	for _, entity := range registry.Result {
		if entity.DisabledBy != nil {
			continue
		}
		if entity.EntityID == ttsEntity && (entity.Platform == "wyoming" || entity.Platform == "piper") {
			localTTS = true
		}
		if entity.DeviceID == route.HADeviceID && strings.HasPrefix(entity.EntityID, "media_player.") {
			if player != "" {
				return fail()
			}
			player = entity.EntityID
		}
	}
	if player == "" || !localTTS {
		return fail()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolveLocal(c.baseURL, "/api/states/"+player), nil)
	if err != nil {
		return fail()
	}
	c.authorize(req)
	response, err := c.client.Do(req)
	if err != nil {
		return fail()
	}
	raw, readErr := readHAResponseBody(response.Body)
	response.Body.Close()
	var state struct {
		State      string `json:"state"`
		Attributes struct {
			Features uint64 `json:"supported_features"`
		} `json:"attributes"`
	}
	if readErr != nil || response.StatusCode != http.StatusOK || json.Unmarshal(raw, &state) != nil || state.State == "unavailable" || state.State == "unknown" {
		return fail()
	}
	if route.Announce && state.Attributes.Features&1048576 == 0 {
		return "", "", 0, errors.New("home_audio_announce_unsupported")
	}
	if !route.Announce && !allowInterrupt {
		return "", "", 0, errors.New("home_audio_interrupt_not_allowed")
	}
	// HA engines advertise provider-specific locale spellings (for example
	// de_DE). Preserve the claim's language while mapping that spelling.
	if wsjson.Write(ctx, conn, map[string]any{"id": 2, "type": "tts/engine/get", "engine_id": ttsEntity}) != nil {
		return fail()
	}
	var engine struct {
		ID      int  `json:"id"`
		Success bool `json:"success"`
		Result  struct {
			Languages []string `json:"supported_languages"`
		} `json:"result"`
	}
	if wsjson.Read(ctx, conn, &engine) != nil || engine.ID != 2 || !engine.Success {
		return fail()
	}
	language, err = homeAudioLanguage(language, engine.Result.Languages)
	if err != nil {
		return fail()
	}
	query := url.Values{"message": {speech}, "language": {language}, "cache": {"false"}, "tts_options": {"{}"}}
	mediaID := "media-source://tts/" + ttsEntity + "?" + query.Encode()
	if wsjson.Write(ctx, conn, map[string]any{"id": 3, "type": "media_source/resolve_media", "media_content_id": mediaID, "expires": 60}) != nil {
		return fail()
	}
	var media struct {
		ID      int  `json:"id"`
		Success bool `json:"success"`
		Result  struct {
			URL  string `json:"url"`
			MIME string `json:"mime_type"`
		} `json:"result"`
	}
	if wsjson.Read(ctx, conn, &media) != nil || media.ID != 3 || !media.Success {
		return fail()
	}
	// Only HA's own finite TTS proxy is read, never an arbitrary media URL.
	mediaURL, err := url.Parse(media.Result.URL)
	if err != nil {
		return fail()
	}
	mediaURL = c.baseURL.ResolveReference(mediaURL)
	if mediaURL.Scheme != c.baseURL.Scheme || mediaURL.Host != c.baseURL.Host || mediaURL.User != nil || mediaURL.Fragment != "" || !strings.HasPrefix(mediaURL.Path, "/api/tts_proxy/") {
		return fail()
	}
	// HA signs media URLs for clients without bearer-header support. Accept
	// only that bounded capability query and keep it out of errors/logs.
	signedQuery, queryErr := url.ParseQuery(mediaURL.RawQuery)
	if queryErr != nil || len(signedQuery) > 1 {
		return fail()
	}
	for name, values := range signedQuery {
		if name != "authSig" || len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 4096 {
			return fail()
		}
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, mediaURL.String(), nil)
	if err != nil {
		return fail()
	}
	c.authorize(req)
	response, err = c.client.Do(req)
	if err != nil {
		return fail()
	}
	defer response.Body.Close()
	audio, err := io.ReadAll(io.LimitReader(response.Body, maxTTSAudioBytes+1))
	if err != nil || response.StatusCode != http.StatusOK || len(audio) > maxTTSAudioBytes {
		return fail()
	}
	duration, err := homeAudioDuration(audio, media.Result.MIME)
	if err != nil {
		return fail()
	}
	return player, mediaURL.String(), duration, nil
}

func homeAudioDuration(audio []byte, mime string) (time.Duration, error) {
	var duration time.Duration
	switch mime {
	case "audio/wav", "audio/x-wav", "audio/wave":
		pcm, rate, channels, _, err := parseBoxMediaWAV(audio)
		if err != nil {
			return 0, errors.New("home_audio_format_unsupported")
		}
		duration = time.Duration(len(pcm)) * time.Second / time.Duration(rate*channels*2)
	case "audio/mpeg", "audio/mp3":
		decoder, err := mp3.NewDecoder(bytes.NewReader(audio))
		if err != nil || decoder.Length() <= 0 {
			return 0, errors.New("home_audio_format_unsupported")
		}
		duration = time.Duration(decoder.Length()) * time.Second / time.Duration(decoder.SampleRate()*4)
	default:
		return 0, errors.New("home_audio_format_unsupported")
	}
	if duration <= 0 || duration > 30*time.Second {
		return 0, errors.New("home_audio_duration_invalid")
	}
	return duration, nil
}

func (c *HomeAssistantClient) playHomeAudio(ctx context.Context, player, mediaURL string, announce bool) error {
	payload, err := json.Marshal(map[string]any{"entity_id": player, "media_content_id": mediaURL, "media_content_type": "music", "announce": announce})
	if err != nil {
		return errors.New("home_audio_dispatch_failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveLocal(c.baseURL, "/api/services/media_player/play_media"), bytes.NewReader(payload))
	if err != nil {
		return errors.New("home_audio_dispatch_failed")
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return errors.New("home_audio_dispatch_indeterminate")
	}
	defer response.Body.Close()
	_, readErr := readHAResponseBody(response.Body)
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("home_audio_dispatch_indeterminate")
	}
	return nil
}

func homeAudioLanguage(language string, supported []string) (string, error) {
	normalize := func(value string) string { return strings.ToLower(strings.ReplaceAll(value, "_", "-")) }
	want := normalize(language)
	base, _, _ := strings.Cut(want, "-")
	candidate := ""
	matches := 0
	for _, value := range supported {
		normalized := normalize(value)
		if normalized == want {
			return value, nil
		}
		if normalized == base {
			candidate = value
			matches = -1
			continue
		}
		code, _, _ := strings.Cut(normalized, "-")
		if code == base && matches >= 0 {
			candidate = value
			matches++
		}
	}
	if matches == -1 || matches == 1 {
		return candidate, nil
	}
	return "", errors.New("home_audio_language_unsupported")
}

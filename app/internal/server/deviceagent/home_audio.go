package deviceagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/homegraph"
)

// HomeAudioOptions is trusted node configuration, never a device request.
// GraphFile is the Cloud v1 document, atomically replaced by its producer.
type HomeAudioOptions struct {
	GraphFile      string
	SpaceID        string
	IdentityKind   string
	TTSEntity      string
	AllowInterrupt bool
}

type homeAudio struct {
	options HomeAudioOptions
	ha      *HomeAssistantClient
	busy    chan struct{}
}

func newHomeAudio(options *HomeAudioOptions, ha HomeAssistant) (*homeAudio, error) {
	if options == nil {
		return nil, nil
	}
	client, ok := ha.(*HomeAssistantClient)
	if !ok || client == nil || !filepath.IsAbs(options.GraphFile) || options.SpaceID == "" || options.TTSEntity == "" || (options.IdentityKind != "edge_device" && options.IdentityKind != "connect_installation") {
		return nil, errors.New("home_audio_config_invalid")
	}
	return &homeAudio{options: *options, ha: client, busy: make(chan struct{}, 1)}, nil
}

func (h *homeAudio) resolve(deviceID, roomID string) (homegraph.Route, error) {
	file, err := os.Open(h.options.GraphFile)
	if err != nil {
		return homegraph.Route{}, errors.New("home_audio_graph_unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > homegraph.MaxDocumentBytes || time.Since(info.ModTime()) > 45*time.Minute || time.Until(info.ModTime()) > time.Minute {
		return homegraph.Route{}, errors.New("home_audio_graph_invalid")
	}
	graph, err := homegraph.Read(file, h.options.SpaceID)
	if err != nil {
		return homegraph.Route{}, err
	}
	for _, device := range graph.Devices {
		if device.Identity.Kind == h.options.IdentityKind && device.Identity.Ref == deviceID && (device.RoomID == nil || *device.RoomID != roomID) {
			return homegraph.Route{}, errors.New("home_audio_room_mismatch")
		}
	}
	return graph.Resolve(h.options.IdentityKind, deviceID)
}

func (h *homeAudio) play(ctx context.Context, route homegraph.Route, result *TurnResult) error {
	// The durable Assist claim is acquired before any speaker dispatch. An
	// uncertain or restarted request never repeats a room announcement.
	if result.Assist.Replayed {
		return errors.New("home_audio_replay_suppressed")
	}
	player, mediaURL, duration, err := h.ha.prepareHomeAudio(ctx, route, h.options.TTSEntity, result.Assist.Speech, result.Language, h.options.AllowInterrupt)
	if err != nil {
		return err
	}
	remaining, ok := ctx.Deadline()
	if !ok || time.Until(remaining) < duration+3*time.Second {
		return errors.New("home_audio_output_expired")
	}
	dispatchErr := h.ha.playHomeAudio(ctx, player, mediaURL, route.Announce)
	// HA's action acknowledgement does not mean the speaker finished. Hold
	// the paired Box in processing through measured media duration plus grace.
	timer := time.NewTimer(duration + 2*time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return dispatchErr
	case <-ctx.Done():
		// Keep the acoustic slot until expiry even if the HTTP caller disconnects.
		<-timer.C
		return errors.New("home_audio_playback_indeterminate")
	}
}

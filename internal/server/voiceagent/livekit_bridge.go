//go:build linux && cgo

package voiceagent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mediapcm "github.com/livekit/media-sdk"
	"github.com/livekit/protocol/livekit"
	protoLogger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	lkmedia "github.com/livekit/server-sdk-go/v2/pkg/media"
	"github.com/pion/webrtc/v4"
)

const (
	defaultLiveKitBridgeConnectTimeout = 5 * time.Second
	liveKitProviderInputSampleRate     = 16000
	liveKitProviderOutputSampleRate    = 24000
	liveKitProviderChannels            = 1
	liveKitAgentTrackName              = "speechkit-agent-audio"
	liveKitAgentParticipantPrefix      = "speechkit-agent"
)

type liveKitRoomClient interface {
	Join(ctx context.Context, url string, info lksdk.ConnectInfo, opts ...lksdk.ConnectOption) error
	PublishTrack(track webrtc.TrackLocal, opts *lksdk.TrackPublicationOptions) error
	Disconnect()
}

type liveKitSDKRoom struct {
	room *lksdk.Room
}

func (r *liveKitSDKRoom) Join(ctx context.Context, url string, info lksdk.ConnectInfo, opts ...lksdk.ConnectOption) error {
	return r.room.JoinWithContext(ctx, url, info, opts...)
}

func (r *liveKitSDKRoom) PublishTrack(track webrtc.TrackLocal, opts *lksdk.TrackPublicationOptions) error {
	_, err := r.room.LocalParticipant.PublishTrack(track, opts)
	return err
}

func (r *liveKitSDKRoom) Disconnect() {
	r.room.Disconnect()
}

// LiveKitMediaBridgeFactory joins the existing session room as an agent
// participant. It subscribes to client microphone Opus tracks, decodes them
// into 16 kHz S16 mono PCM for the realtime provider, and publishes provider
// PCM output as an Opus track back into the room.
type LiveKitMediaBridgeFactory struct {
	Issuer         *LiveKitTokenIssuer
	ConnectTimeout time.Duration

	newRoom       func(*lksdk.RoomCallback) liveKitRoomClient
	newLocalTrack func() (*lkmedia.PCMLocalTrack, error)
}

func NewLiveKitMediaBridgeFactory(issuer *LiveKitTokenIssuer) *LiveKitMediaBridgeFactory {
	return &LiveKitMediaBridgeFactory{Issuer: issuer}
}

func (f *LiveKitMediaBridgeFactory) Start(ctx context.Context, req MediaBridgeRequest) (MediaBridge, error) {
	if f == nil || f.Issuer == nil || !f.Issuer.Enabled() {
		return nil, errors.New("voiceagent: LiveKit media bridge is disabled")
	}
	issuer := f.Issuer
	url := strings.TrimRight(strings.TrimSpace(issuer.URL), "/")
	apiKey := strings.TrimSpace(issuer.APIKey)
	apiSecret := strings.TrimSpace(issuer.APISecret)
	sessionID := strings.TrimSpace(req.SessionID)
	if url == "" || apiKey == "" || apiSecret == "" {
		return nil, errors.New("voiceagent: LiveKit URL/API key/API secret is not configured")
	}
	if sessionID == "" {
		return nil, errors.New("voiceagent: LiveKit media bridge requires a session id")
	}
	if req.Provider == nil {
		return nil, errors.New("voiceagent: LiveKit media bridge requires a provider")
	}

	log := protoLogger.GetLogger()
	bridge := &liveKitMediaBridge{
		provider: req.Provider,
		logger:   log,
	}
	callback := &lksdk.RoomCallback{
		OnDisconnected: func() {
			bridge.closeRemoteTracks()
		},
		OnDisconnectedWithReason: func(_ lksdk.DisconnectionReason) {
			bridge.closeRemoteTracks()
		},
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: bridge.onTrackSubscribed,
		},
	}
	room := f.newRoomClient(callback)
	bridge.room = room

	metadata, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"user_id":    req.Owner.UserID,
		"org_id":     req.Owner.OrgID,
		"role":       "agent",
	})
	if err != nil {
		return nil, err
	}
	connectTimeout := f.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = defaultLiveKitBridgeConnectTimeout
	}
	info := lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            issuer.roomName(sessionID),
		ParticipantName:     "SpeechKit Voice Agent",
		ParticipantIdentity: liveKitAgentIdentity(sessionID),
		ParticipantKind:     lksdk.ParticipantAgent,
		ParticipantMetadata: string(metadata),
		ParticipantAttributes: map[string]string{
			"speechkit.session_id": sessionID,
			"speechkit.role":       "agent",
		},
	}
	if err := room.Join(ctx, url, info,
		lksdk.WithAutoSubscribe(true),
		lksdk.WithConnectTimeout(connectTimeout),
		lksdk.WithLogger(log),
	); err != nil {
		return nil, fmt.Errorf("voiceagent: join LiveKit room: %w", err)
	}

	output, err := f.newProviderOutputTrack(log)
	if err != nil {
		room.Disconnect()
		return nil, fmt.Errorf("voiceagent: create LiveKit output track: %w", err)
	}
	if err := room.PublishTrack(output, &lksdk.TrackPublicationOptions{
		Name:       liveKitAgentTrackName,
		Source:     livekit.TrackSource_MICROPHONE,
		DisableDTX: true,
	}); err != nil {
		_ = output.Close()
		room.Disconnect()
		return nil, fmt.Errorf("voiceagent: publish LiveKit output track: %w", err)
	}
	bridge.output = output
	return bridge, nil
}

func (f *LiveKitMediaBridgeFactory) newRoomClient(callback *lksdk.RoomCallback) liveKitRoomClient {
	if f.newRoom != nil {
		return f.newRoom(callback)
	}
	return &liveKitSDKRoom{room: lksdk.NewRoom(callback)}
}

func (f *LiveKitMediaBridgeFactory) newProviderOutputTrack(log protoLogger.Logger) (*lkmedia.PCMLocalTrack, error) {
	if f.newLocalTrack != nil {
		return f.newLocalTrack()
	}
	return lkmedia.NewPCMLocalTrack(liveKitProviderOutputSampleRate, liveKitProviderChannels, log)
}

func liveKitAgentIdentity(sessionID string) string {
	return liveKitAgentParticipantPrefix + "-" + compactTokenPart(sessionID, 32)
}

type liveKitMediaBridge struct {
	mu           sync.Mutex
	closed       bool
	room         liveKitRoomClient
	provider     LiveProviderAdapter
	output       *lkmedia.PCMLocalTrack
	remoteTracks []*lkmedia.PCMRemoteTrack
	logger       protoLogger.Logger
}

func (b *liveKitMediaBridge) onTrackSubscribed(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	if track == nil || track.Kind() != webrtc.RTPCodecTypeAudio {
		return
	}
	remote, err := lkmedia.NewPCMRemoteTrack(
		track,
		&liveKitProviderWriter{provider: b.provider},
		lkmedia.WithTargetSampleRate(liveKitProviderInputSampleRate),
		lkmedia.WithTargetChannels(liveKitProviderChannels),
		lkmedia.WithLogger(b.logger),
	)
	if err != nil {
		participant := ""
		if rp != nil {
			participant = rp.Identity()
		}
		slog.Warn("voiceagent: LiveKit remote audio track rejected", "participant", participant, "err", err)
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		remote.Close()
		return
	}
	b.remoteTracks = append(b.remoteTracks, remote)
	b.mu.Unlock()
}

func (b *liveKitMediaBridge) SendAudio(data []byte) error {
	sample := pcmBytesToSample(data)
	if len(sample) == 0 {
		return nil
	}
	b.mu.Lock()
	closed := b.closed
	output := b.output
	b.mu.Unlock()
	if closed {
		return errors.New("voiceagent: LiveKit media bridge is closed")
	}
	if output == nil {
		return errors.New("voiceagent: LiveKit output track is not ready")
	}
	return output.WriteSample(sample)
}

func (b *liveKitMediaBridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	room := b.room
	output := b.output
	tracks := b.remoteTracks
	b.room = nil
	b.output = nil
	b.remoteTracks = nil
	b.mu.Unlock()

	for _, track := range tracks {
		track.Close()
	}
	var err error
	if output != nil {
		output.ClearQueue()
		err = output.Close()
	}
	if room != nil {
		room.Disconnect()
	}
	return err
}

func (b *liveKitMediaBridge) closeRemoteTracks() {
	b.mu.Lock()
	tracks := b.remoteTracks
	b.remoteTracks = nil
	b.mu.Unlock()
	for _, track := range tracks {
		track.Close()
	}
}

type liveKitProviderWriter struct {
	provider LiveProviderAdapter
}

func (w *liveKitProviderWriter) WriteSample(sample mediapcm.PCM16Sample) error {
	if len(sample) == 0 {
		return nil
	}
	return w.provider.SendAudio(pcmSampleToBytes(sample))
}

func (w *liveKitProviderWriter) Close() error {
	return w.provider.SendAudioStreamEnd()
}

func pcmBytesToSample(data []byte) mediapcm.PCM16Sample {
	if len(data) < 2 {
		return nil
	}
	sample := make(mediapcm.PCM16Sample, len(data)/2)
	for i := range sample {
		sample[i] = int16(binary.LittleEndian.Uint16(data[i*2:])) //nolint:gosec // PCM16 sample is a bit reinterpretation of audio data, not a numeric overflow
	}
	return sample
}

func pcmSampleToBytes(sample mediapcm.PCM16Sample) []byte {
	if len(sample) == 0 {
		return nil
	}
	out := make([]byte, len(sample)*2)
	_, _ = sample.CopyTo(out)
	return out
}

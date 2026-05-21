package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
)

const (
	sttPhraseWindow            = 3 * time.Second
	sttPhraseMinDuration       = 1200 * time.Millisecond
	sttPhraseScanInterval      = 900 * time.Millisecond
	sttPhraseTranscribeTimeout = 12 * time.Second
	sttPhraseMinLevel          = 0.012
)

type sttPhraseWakeRuntime struct {
	ctx        context.Context
	cancel     context.CancelFunc
	session    audio.Session
	state      *appState
	hkManager  *modeHotkeyManager
	resolved   resolvedWakewordAssets
	transcribe routerTranscriber
	language   string
	aliases    []string
	cooldown   time.Duration
	minHits    int

	mu          sync.Mutex
	pcm         []byte
	lastTrigger time.Time
	consecutive int
	bytesIn     int64
	decodes     int64
}

func launchSTTPhraseWakeword(_ context.Context, state *appState, hkManager *modeHotkeyManager, cfg *config.Config, resolved resolvedWakewordAssets) (*desktopWakeRuntime, error) {
	if state == nil || state.sttRouter == nil {
		return nil, fmt.Errorf("STT phrase match unavailable: no STT router configured")
	}
	session, err := audio.Open(audio.Config{
		Backend:     audio.Backend(cfg.Audio.Backend),
		DeviceID:    cfg.Audio.DeviceID,
		SampleRate:  audio.SampleRate,
		Channels:    audio.Channels,
		FrameSizeMs: 32,
		LatencyHint: "interactive",
	})
	if err != nil {
		return nil, fmt.Errorf("audio open: %w", err)
	}

	ctx, cancel := context.WithCancel(sidecarParentContext())
	rt := &sttPhraseWakeRuntime{
		ctx:       ctx,
		cancel:    cancel,
		session:   session,
		state:     state,
		hkManager: hkManager,
		resolved:  resolved,
		transcribe: routerTranscriber{
			router: newRuntimeDictationTranscriber(state.sttRouter, state),
			state:  state,
		},
		language: strings.TrimSpace(cfg.General.Language),
		aliases:  wakePhraseAliases(cfg.Wakeword, resolved.DisplayPhrase),
		cooldown: time.Duration(maxInt(cfg.Wakeword.CooldownMs, 1500)) * time.Millisecond,
		minHits:  maxInt(cfg.Wakeword.MinConsecutiveFrames, 1),
	}
	session.SetPCMHandler(rt.feedPCM)
	if err := session.Start(); err != nil {
		_ = session.Close()
		cancel()
		return nil, fmt.Errorf("audio start: %w", err)
	}

	done := make(chan struct{})
	go rt.run(done)

	return &desktopWakeRuntime{
		cancel: cancel,
		closeFn: func() {
			cancel()
			_, _ = session.Stop()
			_ = session.Close()
		},
		doneCh: done,
	}, nil
}

func (r *sttPhraseWakeRuntime) feedPCM(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pcm = append(r.pcm, pcm...)
	maxBytes := int(sttPhraseWindow.Seconds()) * audio.SampleRate * audio.BytesPerSample
	if len(r.pcm) > maxBytes {
		r.pcm = append([]byte(nil), r.pcm[len(r.pcm)-maxBytes:]...)
	}
	r.bytesIn += int64(len(pcm))
}

func (r *sttPhraseWakeRuntime) run(done chan<- struct{}) {
	defer close(done)
	scanTicker := time.NewTicker(sttPhraseScanInterval)
	defer scanTicker.Stop()
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-scanTicker.C:
			r.scanOnce()
		case <-heartbeatTicker.C:
			bytes, decodes := r.snapshotAndResetStats()
			r.state.setWakewordStatus(fmt.Sprintf(
				"Listening for \"%s\" → %s via %s (audio %.1f KB/30s, transcribes %d)",
				r.resolved.DisplayPhrase,
				r.resolved.DefaultMode,
				wakewordBackendDisplayName(r.resolved.Backend),
				float64(bytes)/1024,
				decodes,
			))
		}
	}
}

func (r *sttPhraseWakeRuntime) scanOnce() {
	pcm := r.snapshotPCM()
	if len(pcm) < int(sttPhraseMinDuration.Seconds()*float64(audio.SampleRate))*audio.BytesPerSample {
		return
	}
	if audio.PCMLevel(pcm) < sttPhraseMinLevel {
		r.resetConsecutive()
		return
	}

	ctx, cancel := context.WithTimeout(r.ctx, sttPhraseTranscribeTimeout)
	defer cancel()
	transcript, err := r.transcribe.Transcribe(ctx, audio.PCMToWAV(pcm), audio.PCMDurationSecs(pcm), r.language)
	if err != nil {
		if ctx.Err() == nil {
			slog.Debug("wakeword stt phrase transcribe failed", "err", err)
		}
		r.resetConsecutive()
		return
	}
	r.addDecode()
	if !wakePhraseTranscriptMatches(transcript.Text, r.aliases) {
		r.resetConsecutive()
		return
	}

	r.mu.Lock()
	r.consecutive++
	ready := r.consecutive >= r.minHits
	now := time.Now()
	if ready && !r.lastTrigger.IsZero() && now.Sub(r.lastTrigger) < r.cooldown {
		ready = false
	}
	if ready {
		r.lastTrigger = now
		r.consecutive = 0
	}
	r.mu.Unlock()
	if ready {
		dispatchWakewordDetection(r.state, r.hkManager, r.resolved, r.resolved.DisplayPhrase, "stt_phrase", r.resolved.DefaultMode, 1)
	}
}

func (r *sttPhraseWakeRuntime) snapshotPCM() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.pcm...)
}

func (r *sttPhraseWakeRuntime) resetConsecutive() {
	r.mu.Lock()
	r.consecutive = 0
	r.mu.Unlock()
}

func (r *sttPhraseWakeRuntime) addDecode() {
	r.mu.Lock()
	r.decodes++
	r.mu.Unlock()
}

func (r *sttPhraseWakeRuntime) snapshotAndResetStats() (int64, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bytes, decodes := r.bytesIn, r.decodes
	r.bytesIn, r.decodes = 0, 0
	return bytes, decodes
}

func wakePhraseAliases(cfg config.WakewordConfig, displayPhrase string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 6)
	add := func(v string) {
		n := normalizeWakePhraseText(v)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	add(displayPhrase)
	add(stripParenthetical(displayPhrase))
	add(strings.ReplaceAll(strings.TrimSpace(cfg.PhraseID), "_", " "))
	switch strings.ToLower(strings.TrimSpace(cfg.PhraseID)) {
	case "hey_quby":
		add("hey quby")
		add("hey cubi")
		add("hey kubi")
	}
	return out
}

func wakePhraseTranscriptMatches(transcript string, aliases []string) bool {
	normalized := normalizeWakePhraseText(transcript)
	if normalized == "" {
		return false
	}
	for _, alias := range aliases {
		if alias != "" && strings.Contains(normalized, alias) {
			return true
		}
	}
	return false
}

func normalizeWakePhraseText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(value)
	var b strings.Builder
	lastSpace := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func stripParenthetical(value string) string {
	if idx := strings.Index(value, "("); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

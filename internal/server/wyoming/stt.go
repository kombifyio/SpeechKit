//go:build linux

package wyoming

import (
	"bufio"
	"context"
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// sttSession accumulates a single STT turn's PCM. Home Assistant's Assist
// pipeline hands the STT engine already-decoded PCM (the ESPHome uplink + any
// codec is handled inside HA), so a turn is: transcribe → audio-start → N×
// audio-chunk → audio-stop, then we run the batch router. This mirrors how
// wyoming-faster-whisper buffers to audio-stop before transcribing.
type sttSession struct {
	buf      []byte
	rate     int
	width    int
	channels int
	language string
	active   bool
}

func (s *sttSession) begin(language string) {
	s.buf = s.buf[:0]
	s.rate, s.width, s.channels = 0, 0, 0
	s.language = language
	s.active = true
}

func (s *sttSession) start(a AudioStart) {
	s.rate, s.width, s.channels = a.Rate, a.Width, a.Channels
	s.buf = s.buf[:0]
	s.active = true
}

func (s *sttSession) appendPCM(pcm []byte) {
	if !s.active || len(pcm) == 0 {
		return
	}
	s.buf = append(s.buf, pcm...)
}

// finishSTT canonicalizes the buffered PCM, runs the batch router, and writes a
// transcript event. An empty transcript (no STT configured, no audio, or an
// error) is a valid Wyoming reply that HA tolerates. Returns false only on a
// write failure so the caller drops the connection.
func (s *Server) finishSTT(ctx context.Context, bw *bufio.Writer, sess *sttSession) bool {
	if !sess.active {
		return true
	}
	sess.active = false

	if s.opts.STT == nil || len(sess.buf) == 0 {
		return s.writeTranscript(bw, "")
	}
	canonical, err := s.canonicalPCM(ctx, sess)
	if err != nil {
		slog.Warn("wyoming: stt canonicalize failed", "err", err)
		return s.writeTranscript(bw, "")
	}
	durationSecs := float64(len(canonical)) / float64(audio.TargetSampleRate*audio.TargetBytesPerSample)
	res, err := s.opts.STT.Route(ctx, canonical, durationSecs, stt.TranscribeOpts{Language: sess.language})
	if err != nil || res == nil {
		slog.Warn("wyoming: stt route failed", "err", err)
		return s.writeTranscript(bw, "")
	}
	return s.writeTranscript(bw, res.Text)
}

// canonicalPCM returns 16 kHz mono S16LE PCM. HA Assist normally already sends
// that (fast path: use the buffer directly); otherwise the raw PCM is wrapped
// in a WAV header and run through the shared ingress canonicalizer, reusing its
// downmix/resample instead of reimplementing them.
func (s *Server) canonicalPCM(ctx context.Context, sess *sttSession) ([]byte, error) {
	if (sess.rate == 0 || sess.rate == audio.TargetSampleRate) &&
		(sess.width == 0 || sess.width == audio.TargetBytesPerSample) &&
		(sess.channels == 0 || sess.channels == audio.TargetChannels) {
		return sess.buf, nil
	}
	wav, err := pcmToWAV(sess.buf, sess.rate, sess.channels, sess.width)
	if err != nil {
		return nil, err
	}
	decoded, err := audio.DecodeWithLimits(ctx, wav, "audio/wav", s.opts.DecodeLimits)
	if err != nil {
		return nil, err
	}
	return decoded.PCM, nil
}

func (s *Server) writeTranscript(bw *bufio.Writer, text string) bool {
	ev, err := transcriptEvent(text)
	if err != nil {
		slog.Warn("wyoming: build transcript", "err", err)
		return true
	}
	return s.write(bw, ev)
}

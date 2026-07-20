//go:build linux

package wyoming

import (
	"bufio"
	"context"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// ttsChunkBytes is the PCM payload size per audio-chunk on the TTS path. Small
// enough to stream smoothly; well under the read cap.
const ttsChunkBytes = 2048

// synthesize runs the TTS router for a `synthesize` event and streams the
// result back as audio-start → N× audio-chunk → audio-stop. Returns false only
// on a write failure so the caller drops the connection.
func (s *Server) synthesize(ctx context.Context, bw *bufio.Writer, syn Synthesize) bool {
	if s.opts.TTS == nil || strings.TrimSpace(syn.Text) == "" {
		return true
	}
	voice := s.opts.DefaultVoice
	locale := ""
	if syn.Voice != nil {
		if v := strings.TrimSpace(syn.Voice.Name); v != "" {
			voice = v
		}
		locale = strings.TrimSpace(syn.Voice.Language)
	}

	res, err := s.opts.TTS.Synthesize(ctx, syn.Text, tts.SynthesizeOpts{
		Voice:  voice,
		Locale: locale,
		Format: "wav", // request PCM-friendly output; mp3 is decoded as a fallback
	})
	if err != nil || res == nil {
		slog.Warn("wyoming: tts synthesize failed", "err", err)
		return true
	}
	pcm, rate, width, channels, err := ttsPCM(ctx, res)
	if err != nil {
		slog.Warn("wyoming: tts decode failed", "err", err, "format", res.Format)
		return true
	}

	if !s.reply(bw, TypeAudioStart, AudioStart{Rate: rate, Width: width, Channels: channels}) {
		return false
	}
	for off := 0; off < len(pcm); off += ttsChunkBytes {
		end := off + ttsChunkBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		ev, cerr := audioChunkEvent(rate, width, channels, pcm[off:end])
		if cerr != nil {
			slog.Warn("wyoming: build tts chunk", "err", cerr)
			return true
		}
		if !s.write(bw, ev) {
			return false
		}
	}
	return s.reply(bw, TypeAudioStop, AudioStop{})
}

// ttsPCM extracts raw PCM and its format from a TTS result. WAV is parsed; bare
// PCM is used as-is; anything else (e.g. mp3) is decoded to 16 kHz mono S16LE.
func ttsPCM(ctx context.Context, res *tts.Result) (pcm []byte, rate, width, channels int, err error) {
	switch strings.ToLower(strings.TrimSpace(res.Format)) {
	case "wav", "wave":
		p, r, ch, wd, perr := parseWAV(res.Audio)
		if perr != nil {
			return nil, 0, 0, 0, perr
		}
		return p, r, wd, ch, nil
	case "pcm", "pcm16", "l16", "linear16":
		rate = res.SampleRate
		if rate <= 0 {
			rate = audio.TargetSampleRate
		}
		return res.Audio, rate, audio.TargetBytesPerSample, audio.TargetChannels, nil
	default:
		decoded, derr := audio.DecodeWithLimits(ctx, res.Audio, "audio/mpeg", audio.DecodeLimits{})
		if derr != nil {
			return nil, 0, 0, 0, derr
		}
		return decoded.PCM, audio.TargetSampleRate, audio.TargetBytesPerSample, audio.TargetChannels, nil
	}
}

//go:build linux

package voiceagent

// Local second stage of the wake word check: the on-box Parakeet sidecar
// (speechkit-stt-sherpa) transcribes the first seconds, the cloud batch
// verifier answers only when the sidecar is unavailable. One local pass
// replaces the two or four parallel cloud passes, and the verdict waits for
// 1.7 s of audio instead of 3, so the provider starts about 1.3 s sooner.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	// The wake phrase ends inside the device's 1.5 s pre-roll, so 1.7 s hold
	// it with a little room for the first live words after it.
	wakeLocalAudioBytes = wakePCMBytesPerSecond * 17 / 10
	// The sidecar answers a 1.7 s clip in 0.25 to 0.4 s; a local pass that takes
	// longer than this hands over to the cloud verifier.
	wakeLocalTimeout = 1500 * time.Millisecond
	// A phonetic code matches words of at least this many letters: "Kampf"
	// reaches the code of "kombify" (4613) and is an everyday German word.
	wakeLocalCodeMinLetters = 6
)

// DefaultLocalWakePhrases are the generic phrases as the unbiased Parakeet
// recognition writes them (measured 2026-10-25 on ten TTS voices). The
// deployment's own product phrases and its entries with the same id add
// their cores, codes and weak words to them (voiceagent_wiring.go); they do
// not replace them.
func DefaultLocalWakePhrases() map[string]WakePhrase {
	return map[string]WakePhrase{
		"hey_kubi":   {Cores: []string{"kubi", "cubi"}},
		"hey_jarvis": {Cores: []string{"jarvis", "dscharvis"}, Codes: []string{"738"}},
	}
}

type localWakeVerifier struct {
	stt      wakeTranscriber
	phrases  map[string]WakePhrase
	fallback WakeVerifier
}

// NewLocalWakeVerifier verifies wake candidates with one unbiased local
// transcription of the first 1.7 s. The transcriber must return word times
// (Result.Words) so the phrase can be cut off the audio. fallback, when set,
// decides whenever the local pass fails or times out.
func NewLocalWakeVerifier(t wakeTranscriber, phrases map[string]WakePhrase, fallback WakeVerifier) WakeVerifier {
	if t == nil {
		return fallback
	}
	norm := make(map[string]WakePhrase, len(phrases))
	for id, p := range phrases {
		n := WakePhrase{Codes: p.Codes}
		for _, w := range p.Weak {
			n.Weak = append(n.Weak, normalizeWakeWord(w))
		}
		for _, c := range p.Cores {
			n.Cores = append(n.Cores, normalizeWakeWord(c))
		}
		norm[strings.ToLower(strings.TrimSpace(id))] = n
	}
	return &localWakeVerifier{stt: t, phrases: norm, fallback: fallback}
}

// localReady reports whether the sidecar can take a request now. A
// transcriber that cannot say counts as ready.
func (v *localWakeVerifier) localReady() bool {
	r, ok := v.stt.(interface{ Ready() bool })
	return !ok || r.Ready()
}

// WakeWindowBytes is the audio the adapter collects before asking: 1.7 s for
// the local pass, the cloud verifier's own 3 s while the sidecar is not
// ready, so the fallback never decides on the shorter window.
func (v *localWakeVerifier) WakeWindowBytes() int {
	if v.fallback != nil && !v.localReady() {
		return wakeVerifyAudioBytes
	}
	return wakeLocalAudioBytes
}

var errLocalWakeNotReady = errors.New("voiceagent: local wake STT is not ready")

func (v *localWakeVerifier) VerifyWake(ctx context.Context, pcm []byte, phrase, locale string) (WakeVerdict, error) {
	if !v.localReady() {
		// Starting, crashed or stopped: no wait. The adapter tops the audio up
		// to the cloud window first, in case the sidecar went down after the
		// shorter window was chosen.
		if v.fallback == nil {
			return WakeVerdict{}, errLocalWakeNotReady
		}
		return WakeVerdict{Phrase: phrase, Undecided: true}, nil
	}
	verdict, err := v.verifyLocal(ctx, pcm, phrase)
	if err != nil {
		slog.Warn("voiceagent: local wake verification unavailable", "phrase", phrase, "err", err)
		if v.fallback == nil {
			return WakeVerdict{}, err
		}
		return WakeVerdict{Phrase: verdict.Phrase, Undecided: true}, nil
	}
	verdict.Stage = "local"
	if verdict.Accepted && verdict.Weak {
		// A bare "Kombi" is an everyday German word. The local pass alone never
		// accepts on it: the cloud pass decides, and without one it is a reject.
		if v.fallback == nil {
			return WakeVerdict{Phrase: verdict.Phrase, Stage: "local"}, nil
		}
		return WakeVerdict{Phrase: verdict.Phrase, Undecided: true, Weak: true}, nil
	}
	return verdict, nil
}

// VerifyWakeCloud runs the cloud verifier on its own window.
func (v *localWakeVerifier) VerifyWakeCloud(ctx context.Context, pcm []byte, phrase, locale string) (WakeVerdict, error) {
	if v.fallback == nil {
		return WakeVerdict{}, errors.New("voiceagent: no cloud wake verifier")
	}
	verdict, err := v.fallback.VerifyWake(ctx, pcm, phrase, locale)
	verdict.Stage = "cloud"
	return verdict, err
}

func (v *localWakeVerifier) verifyLocal(ctx context.Context, pcm []byte, phrase string) (WakeVerdict, error) {
	if len(pcm) > wakeLocalAudioBytes {
		pcm = pcm[:wakeLocalAudioBytes]
	}
	id := strings.ToLower(strings.TrimSpace(phrase))
	spec, ok := v.phrases[id]
	if !ok || (len(spec.Cores) == 0 && len(spec.Codes) == 0) {
		// An unknown phrase has no local rule; the cloud verifier knows the
		// last-word fallback.
		return WakeVerdict{}, errors.New("voiceagent: no local rule for wake phrase " + id)
	}
	lctx, cancel := context.WithTimeout(ctx, wakeLocalTimeout)
	defer cancel()
	res, err := v.stt.Transcribe(lctx, stt.EnsureTranscriptionWAV(pcm), stt.TranscribeOpts{})
	if err != nil {
		return WakeVerdict{}, err
	}
	if res == nil {
		return WakeVerdict{}, errors.New("voiceagent: empty local wake transcription")
	}
	return matchWake(id, wakeRule{cores: spec.Cores, codes: spec.Codes, weak: spec.Weak,
		codeMinLetters: wakeLocalCodeMinLetters, singleWordCodes: true}, res), nil
}

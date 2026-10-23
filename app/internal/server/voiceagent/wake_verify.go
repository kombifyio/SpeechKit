//go:build linux

package voiceagent

// Two-stage wake word verification before provider start.
// Devices run their wake word detector at a low cutoff and only nominate a
// candidate; the start frame carries it and the device sends its pre-roll
// plus the following speech once the server reports state "verifying". The
// server transcribes the first seconds (no provider retention) and matches
// the phrase before any provider connects. A rejected candidate ends the
// session before Connect, so it settles at zero like any unstarted session.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	wakePCMBytesPerSecond = 16000 * 2
	// The verdict uses the first 3 s: pre-roll, phrase and the start of the
	// request. Devices send their buffer at once, so this arrives quickly.
	wakeVerifyAudioBytes = 3 * wakePCMBytesPerSecond
	wakeVerifyMinBytes   = wakePCMBytesPerSecond / 2
	wakeCollectTimeout   = 6 * time.Second
	wakeVerifyTimeout    = 4 * time.Second
)

// WakeVerifier decides whether pcm (16 kHz S16LE mono) starts with the
// candidate phrase.
type WakeVerifier interface {
	VerifyWake(ctx context.Context, pcm []byte, phrase, locale string) (WakeVerdict, error)
}

// WakeVerdict is the second-stage decision.
type WakeVerdict struct {
	Accepted bool
	// Phrase is the canonical phrase id that matched.
	Phrase string
	// TrimBytes is the audio up to the end of the phrase. It is not replayed
	// to the provider, so the agent hears the request, not the wake word.
	TrimBytes int
}

// wakeTranscriber is the batch STT the verifier needs (Deepgram in
// production, a fake in tests).
type wakeTranscriber interface {
	Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error)
}

type sttWakeVerifier struct {
	stt      wakeTranscriber
	phrases  map[string]WakePhrase
	keyterms []string
}

// NewSTTWakeVerifier verifies wake candidates with batch transcriptions of
// the first seconds and a tolerant phrase match. The transcriber must not
// retain audio (Deepgram: NoStore). keyterms bias the first pass towards
// the deployment's wake words.
func NewSTTWakeVerifier(t wakeTranscriber, phrases map[string]WakePhrase, keyterms []string) WakeVerifier {
	if t == nil {
		return nil
	}
	norm := make(map[string]WakePhrase, len(phrases))
	for id, p := range phrases {
		n := WakePhrase{ConfirmCodes: p.ConfirmCodes}
		for _, c := range p.Cores {
			n.Cores = append(n.Cores, normalizeWakeWord(c))
		}
		for _, c := range p.Confirm {
			n.Confirm = append(n.Confirm, normalizeWakeWord(c))
		}
		norm[strings.ToLower(strings.TrimSpace(id))] = n
	}
	return &sttWakeVerifier{stt: t, phrases: norm, keyterms: keyterms}
}

// WakePhrase describes how the second stage recognises one phrase. The
// deployment supplies its phrases ([voice_agent.wake] in the server config);
// a phrase id without an entry falls back to its last word as the core.
//
// Cores is the distinctive word as recognition writes it with keyterm
// bias. The greeting is not checked: German recognition turns "Hey" into
// "Herr" or "Hee", and stage one already heard the whole phrase. Words of
// five letters or fewer must match exactly; longer ones may differ by
// about one letter in four.
//
// Confirm or ConfirmCodes, when set, must also match in an unbiased
// transcription, at the same word: keyterm bias spells a coined name
// reliably but also bends near words onto short names (a prompted "Kubi"
// turned "Kuba", "Kobe" and "Kuhn" into "Kubi" in the 2026-10-22
// measurement). ConfirmCodes are Cologne phonetic codes of the unbiased
// words, for names unbiased recognition cannot spell.
type WakePhrase struct {
	Cores        []string
	Confirm      []string
	ConfirmCodes []string
}

// wakeLanguages transcribes in the session language and in Deepgram's
// code-switching "multi" model at the same time; either match counts.
// English phrases inside German speech (and the reverse) need both.
func wakeLanguages(locale string) []string {
	lang := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i]
	}
	if lang == "" || lang == "multi" {
		lang = "de"
	}
	return []string{lang, "multi"}
}

func (v *sttWakeVerifier) phraseFor(phrase string) (string, WakePhrase) {
	id := strings.ToLower(strings.TrimSpace(phrase))
	if p, ok := v.phrases[id]; ok && len(p.Cores) > 0 {
		return id, p
	}
	fields := strings.Fields(strings.ReplaceAll(id, "_", " "))
	if len(fields) == 0 {
		return id, WakePhrase{}
	}
	return id, WakePhrase{Cores: []string{normalizeWakeWord(fields[len(fields)-1])}}
}

func (v *sttWakeVerifier) VerifyWake(ctx context.Context, pcm []byte, phrase, locale string) (WakeVerdict, error) {
	if len(pcm) > wakeVerifyAudioBytes {
		pcm = pcm[:wakeVerifyAudioBytes]
	}
	id, spec := v.phraseFor(phrase)
	if len(spec.Cores) == 0 {
		return WakeVerdict{Phrase: id}, nil
	}
	type job struct {
		confirm bool
		opts    stt.TranscribeOpts
	}
	var jobs []job
	for _, lang := range wakeLanguages(locale) {
		jobs = append(jobs, job{opts: stt.TranscribeOpts{Language: lang, Keyterms: v.keyterms}})
		if len(spec.Confirm) > 0 || len(spec.ConfirmCodes) > 0 {
			jobs = append(jobs, job{confirm: true, opts: stt.TranscribeOpts{Language: lang}})
		}
	}
	type result struct {
		confirm bool
		match   WakeVerdict
		err     error
	}
	results := make(chan result, len(jobs))
	for _, j := range jobs {
		go func() {
			res, err := v.stt.Transcribe(ctx, pcm, j.opts)
			if err == nil && res == nil {
				err = errors.New("voiceagent: empty wake transcription")
			}
			if err != nil {
				results <- result{confirm: j.confirm, err: err}
				return
			}
			switch {
			case !j.confirm:
				results <- result{match: matchWake(id, spec.Cores, nil, res)}
			default:
				results <- result{confirm: true, match: matchWake(id, spec.Confirm, spec.ConfirmCodes, res)}
			}
		}()
	}
	var heard, confirmed []WakeVerdict
	var firstErr error
	answered := 0
	for range jobs {
		r := <-results
		switch {
		case r.err != nil:
			if firstErr == nil {
				firstErr = r.err
			}
		case r.confirm:
			answered++
			if r.match.Accepted {
				confirmed = append(confirmed, r.match)
			}
		default:
			answered++
			if r.match.Accepted {
				heard = append(heard, r.match)
			}
		}
	}
	if answered == 0 {
		return WakeVerdict{}, firstErr
	}
	needConfirm := len(spec.Confirm) > 0 || len(spec.ConfirmCodes) > 0
	for _, h := range heard {
		if !needConfirm {
			return h, nil
		}
		for _, c := range confirmed {
			if wakeSameWord(h, c) {
				return h, nil
			}
		}
	}
	return WakeVerdict{Phrase: id}, nil
}

// wakeSameWord requires the biased and the unbiased match to end within
// half a second: a short word joined with the next one can reach the same
// phonetic code, but not where the biased transcript heard its match.
func wakeSameWord(a, b WakeVerdict) bool {
	if a.TrimBytes == 0 || b.TrimBytes == 0 {
		return true
	}
	d := a.TrimBytes - b.TrimBytes
	if d < 0 {
		d = -d
	}
	return d <= wakePCMBytesPerSecond/2
}

// normalizeWakeWord lowercases, folds umlauts and drops everything but
// letters and digits.
func normalizeWakeWord(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'ä':
			b.WriteString("ae")
		case 'ö':
			b.WriteString("oe")
		case 'ü':
			b.WriteString("ue")
		case 'ß':
			b.WriteString("ss")
		default:
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// wakeSimilar is a normalized Levenshtein similarity of two letter strings.
func wakeSimilar(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	longest := max(len(ra), len(rb))
	return 1 - float64(prev[len(rb)])/float64(longest)
}

const (
	wakeMatchThreshold = 0.75
	wakeExactMaxLen    = 5
)

// wakeCoreMatches compares a candidate with the cores. Only a single word
// may differ from a core; words joined across a split must match exactly,
// otherwise a short word plus the next word passes for a longer core.
func wakeCoreMatches(candidate string, cores []string, joined bool) bool {
	for _, core := range cores {
		if candidate == core {
			return true
		}
		if !joined && len(core) > wakeExactMaxLen && wakeSimilar(candidate, core) >= wakeMatchThreshold {
			return true
		}
	}
	return false
}

func wakeCodeMatches(candidate string, codes []string) bool {
	if len(codes) == 0 {
		return false
	}
	code := colognePhonetic(candidate)
	for _, c := range codes {
		if code == c {
			return true
		}
	}
	return false
}

// colognePhonetic is the Kölner Phonetik code of a normalized word (letters
// a-z after normalizeWakeWord): adjacent duplicates collapse and zeros are
// dropped except at the start, which is then trimmed too.
func colognePhonetic(w string) string {
	r := []rune(w)
	var out []byte
	at := func(i int) rune {
		if i < 0 || i >= len(r) {
			return 0
		}
		return r[i]
	}
	for i, c := range r {
		prev, next := at(i-1), at(i+1)
		var d byte
		switch c {
		case 'a', 'e', 'i', 'j', 'o', 'u', 'y':
			d = '0'
		case 'h':
			continue
		case 'b':
			d = '1'
		case 'p':
			d = '1'
			if next == 'h' {
				d = '3'
			}
		case 'd', 't':
			d = '2'
			if strings.ContainsRune("csz", next) {
				d = '8'
			}
		case 'f', 'v', 'w':
			d = '3'
		case 'g', 'k', 'q':
			d = '4'
		case 'c':
			switch {
			case i == 0 && strings.ContainsRune("ahkloqrux", next):
				d = '4'
			case i > 0 && strings.ContainsRune("ahkoqux", next) && !strings.ContainsRune("sz", prev):
				d = '4'
			default:
				d = '8'
			}
		case 'x':
			if strings.ContainsRune("ckq", prev) {
				d = '8'
			} else {
				out = append(out, '4')
				d = '8'
			}
		case 'l':
			d = '5'
		case 'm', 'n':
			d = '6'
		case 'r':
			d = '7'
		case 's', 'z':
			d = '8'
		default:
			continue
		}
		if len(out) > 0 && out[len(out)-1] == d {
			continue
		}
		out = append(out, d)
	}
	var b strings.Builder
	for _, d := range out {
		if d != '0' {
			b.WriteByte(d)
		}
	}
	return b.String()
}

// matchWake looks for one of cores among the first words of the
// transcript, also split over up to three words.
func matchWake(id string, cores, codes []string, res *stt.Result) WakeVerdict {
	type word struct {
		text  string
		endMs int64
	}
	var words []word
	if len(res.Words) > 0 {
		for _, w := range res.Words {
			if t := normalizeWakeWord(w.Text); t != "" {
				words = append(words, word{text: t, endMs: w.EndMs})
			}
		}
	} else {
		for _, f := range strings.Fields(res.Text) {
			if t := normalizeWakeWord(f); t != "" {
				words = append(words, word{text: t, endMs: -1})
			}
		}
	}
	// The phrase sits at the start of the audio, after the pre-roll; a few
	// leading words of background speech are tolerated.
	const searchWords = 8
	for start := 0; start < len(words) && start < searchWords; start++ {
		var joined strings.Builder
		for n := 1; n <= 3 && start+n <= len(words); n++ {
			joined.WriteString(words[start+n-1].text)
			if !wakeCoreMatches(joined.String(), cores, n > 1) && !wakeCodeMatches(joined.String(), codes) {
				continue
			}
			trim := 0
			if end := words[start+n-1].endMs; end > 0 {
				trim = int(end * wakePCMBytesPerSecond / 1000)
				trim -= trim % 2
			}
			return WakeVerdict{Accepted: true, Phrase: id, TrimBytes: trim}
		}
	}
	return WakeVerdict{Phrase: id}
}

// collectWakeAudio reads uplink audio until the verdict window is full. A
// stop or audio_end ends it early; other text frames are ignored here.
func (a *Adapter) collectWakeAudio(ctx context.Context) ([]byte, bool, error) {
	readCtx, cancel := context.WithTimeout(ctx, wakeCollectTimeout)
	defer cancel()
	buf := make([]byte, 0, wakeVerifyAudioBytes+4096)
	for len(buf) < wakeVerifyAudioBytes {
		typ, data, err := a.Conn.Read(readCtx)
		if err != nil {
			return buf, false, err
		}
		if typ == websocket.MessageBinary {
			buf = append(buf, data...)
			continue
		}
		var env envelope
		if json.Unmarshal(data, &env) == nil && (env.Type == MsgStop || env.Type == MsgAudioEnd) {
			return buf, env.Type == MsgStop, nil
		}
	}
	return buf, false, nil
}

// verifyWake runs the second stage. It returns the audio to replay to the
// provider after Connect, and false when the session must end (rejected,
// stopped or broken). Verifier errors fail open: the device already heard
// a candidate, and a missed request costs the user more than one
// unverified session.
func (a *Adapter) verifyWake(ctx context.Context, start StartFrame) ([]byte, bool, string) {
	a.sendJSON(ctx, StateFrame{Type: MsgState, State: "verifying"})
	audio, stopped, err := a.collectWakeAudio(ctx)
	if err != nil || stopped {
		return nil, false, "client"
	}
	if len(audio) < wakeVerifyMinBytes {
		a.sendJSON(ctx, WakeResultFrame{Type: MsgWakeRejected, Phrase: start.Wake.Phrase, Reason: "no_audio"})
		return nil, false, "wake_rejected"
	}
	vctx, cancel := context.WithTimeout(ctx, wakeVerifyTimeout)
	defer cancel()
	began := time.Now()
	verdict, err := a.WakeVerifier.VerifyWake(vctx, audio, start.Wake.Phrase, start.Locale)
	elapsed := time.Since(began).Milliseconds()
	if err != nil {
		slog.Warn("voiceagent: wake verification unavailable, accepting candidate", "phrase", start.Wake.Phrase, "ms", elapsed, "err", err)
		verdict = WakeVerdict{Accepted: true, Phrase: start.Wake.Phrase}
	}
	slog.Info("voiceagent: wake candidate", "phrase", start.Wake.Phrase, "score", start.Wake.Score, "accepted", verdict.Accepted, "ms", elapsed, "audio_ms", len(audio)*1000/wakePCMBytesPerSecond)
	if !verdict.Accepted {
		a.sendJSON(ctx, WakeResultFrame{Type: MsgWakeRejected, Phrase: verdict.Phrase, Reason: "no_match"})
		return nil, false, "wake_rejected"
	}
	a.sendJSON(ctx, WakeResultFrame{Type: MsgWakeVerified, Phrase: verdict.Phrase})
	if verdict.TrimBytes > 0 && verdict.TrimBytes < len(audio) {
		audio = audio[verdict.TrimBytes:]
	}
	return audio, true, ""
}

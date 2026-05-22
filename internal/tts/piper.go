package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Piper implements Provider via the `piper` command-line binary. The
// binary writes a WAV PCM stream to stdout when invoked with
//
//	piper --model <voice.onnx> --output-raw < input.txt
//
// or `--output_file -` for a WAV-wrapped stream. Phase 3 of the
// voice-companion roadmap uses Piper as the all-local TTS so the
// Voice-Companion can talk without any cloud key.
//
// The implementation does NOT bundle voice models. Operators run
// `scripts/prepare-piper-voices.ps1` once to download the desired
// voices into PiperOpts.VoiceDir. Default voice maps to <VoiceDir>/
// en_US-amy-medium.onnx; locale "de" looks up de_DE-thorsten-medium.
type Piper struct {
	binary   string
	voiceDir string
	defaults map[string]string // locale code → onnx filename
	timeout  time.Duration
}

// PiperOpts configures the local Piper subprocess.
type PiperOpts struct {
	// Binary is the absolute or PATH-relative `piper` executable.
	// Empty defaults to "piper" (must be on $PATH).
	Binary string

	// VoiceDir is the filesystem root containing the .onnx voice
	// model files. Required.
	VoiceDir string

	// DefaultVoices maps a locale code (e.g. "en", "de") to a
	// voice-model filename inside VoiceDir. Falls back to the en_US
	// Amy medium voice when an entry is missing.
	DefaultVoices map[string]string

	// Timeout caps the subprocess execution. Zero defaults to 30 s
	// — generous because Piper warm-load on CPU can take several
	// seconds for a first synthesis.
	Timeout time.Duration
}

// NewPiper validates the options and returns a ready Provider. The
// voice directory must already exist; missing voice models surface
// as a clear "voice file not found" error at Synthesize time, not
// at construction, so a missing en_DE voice does not prevent the
// provider from answering en_US requests.
func NewPiper(opts PiperOpts) (*Piper, error) {
	binaryPath := strings.TrimSpace(opts.Binary)
	if binaryPath == "" {
		binaryPath = "piper"
	}
	voiceDir := strings.TrimSpace(opts.VoiceDir)
	if voiceDir == "" {
		return nil, errors.New("piper: VoiceDir is required")
	}
	defaults := map[string]string{
		"en": "en_US-amy-medium.onnx",
		"de": "de_DE-thorsten-medium.onnx",
	}
	for k, v := range opts.DefaultVoices {
		defaults[normaliseLocaleShort(k)] = v
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Piper{
		binary:   binaryPath,
		voiceDir: voiceDir,
		defaults: defaults,
		timeout:  timeout,
	}, nil
}

// Name returns the provider identifier.
func (*Piper) Name() string { return "piper" }

// Health verifies the piper binary can be located. Voice-model
// presence is not checked here — operators may have a partial set
// installed and we still want /readyz to be green for the locales
// they DO have. Synthesize surfaces missing-voice errors per
// request.
func (p *Piper) Health(ctx context.Context) error {
	if p == nil {
		return errors.New("piper: nil provider")
	}
	cmd := exec.CommandContext(ctx, p.binary, "--version") //nolint:gosec // operator-supplied binary path
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("piper: probing binary %q: %w", p.binary, err)
	}
	return nil
}

// Synthesize runs the piper subprocess for one utterance. The
// returned Result.Audio is a complete RIFF/WAVE PCM blob suitable
// for direct playback or HTTP delivery.
func (p *Piper) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if p == nil {
		return nil, errors.New("piper: nil provider")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("piper: empty text")
	}

	model, err := p.resolveVoice(opts.Voice, opts.Locale)
	if err != nil {
		return nil, err
	}

	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// Piper reads UTF-8 text from stdin and writes a WAV stream to
	// stdout when --output_file is "-". Some Piper builds spell the
	// flag --output-file; the long form is consistent across both.
	cmd := exec.CommandContext(cctx, //nolint:gosec // operator-supplied binary + voice paths
		p.binary,
		"--model", model,
		"--output_file", "-",
	)
	cmd.Stdin = strings.NewReader(text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("piper: run %q: %w (stderr: %s)", p.binary, err, strings.TrimSpace(stderr.String()))
	}
	audio := stdout.Bytes()
	if len(audio) < 44 {
		return nil, errors.New("piper: stdout missing WAV header")
	}
	sampleRate := readWAVSampleRate(audio)

	return &Result{
		Audio:      audio,
		Format:     "wav",
		SampleRate: sampleRate,
		Duration:   estimateWAVDuration(audio, sampleRate),
		Provider:   "piper",
		Voice:      filepath.Base(model),
	}, nil
}

// resolveVoice picks the .onnx model file for a Synthesize call.
// Explicit `opts.Voice` wins; falls back to the locale default;
// falls back to the en default. Returns the resolved absolute path
// inside VoiceDir.
func (p *Piper) resolveVoice(requested, locale string) (string, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		// Allow callers to pass either "en_US-amy-medium" or the
		// full filename; normalise to .onnx.
		if !strings.HasSuffix(requested, ".onnx") {
			requested += ".onnx"
		}
		return filepath.Join(p.voiceDir, requested), nil
	}
	key := normaliseLocaleShort(locale)
	if voice := p.defaults[key]; voice != "" {
		return filepath.Join(p.voiceDir, voice), nil
	}
	if voice := p.defaults["en"]; voice != "" {
		return filepath.Join(p.voiceDir, voice), nil
	}
	return "", fmt.Errorf("piper: no default voice configured for locale %q", locale)
}

// normaliseLocaleShort drops the region and lowercases — "de-DE"
// → "de", "EN_us" → "en". Used by the default-voice lookup so a
// caller passing the BCP-47 form still matches.
func normaliseLocaleShort(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if l == "" {
		return "en"
	}
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	return l
}

// readWAVSampleRate extracts the sample-rate field from a canonical
// WAV header. Returns 16000 (Piper's default) when the header looks
// malformed; the caller has already validated len(audio) >= 44.
func readWAVSampleRate(audio []byte) int {
	if len(audio) < 28 {
		return 16000
	}
	// RIFF header layout: bytes 24-27 hold the sample rate (little
	// endian uint32).
	rate := binary.LittleEndian.Uint32(audio[24:28])
	if rate == 0 {
		return 16000
	}
	return int(rate)
}

// PiperVoiceInfo is one entry from a voice-directory scan. Filename is
// the basename inside VoiceDir (e.g. "en_US-amy-medium.onnx"). The
// remaining fields are best-effort parses of the rhasspy/piper-voices
// naming convention <lang>_<REGION>-<name>-<quality>.onnx; when the
// filename does not follow that pattern only Filename is populated and
// the other fields remain empty.
type PiperVoiceInfo struct {
	Filename string `json:"filename"`
	Locale   string `json:"locale"`  // short code, e.g. "en", "de"
	Region   string `json:"region"`  // e.g. "US", "DE"; empty when missing
	Name     string `json:"name"`    // e.g. "amy", "thorsten"
	Quality  string `json:"quality"` // e.g. "low", "medium", "high"
	SizeKB   int    `json:"size_kb"`
}

// ListPiperVoices scans voiceDir for *.onnx files and parses each
// filename using the piper-voices naming convention. The list is
// sorted by Filename so callers can present a stable UI. Returns an
// empty slice (not an error) when voiceDir is empty or missing — the
// UI surfaces "no voices installed" rather than a hard error.
func ListPiperVoices(voiceDir string) ([]PiperVoiceInfo, error) {
	voiceDir = strings.TrimSpace(voiceDir)
	if voiceDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(voiceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("piper: scan %q: %w", voiceDir, err)
	}
	var voices []PiperVoiceInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".onnx") {
			continue
		}
		voice := parsePiperVoiceFilename(name)
		if info, err := e.Info(); err == nil {
			voice.SizeKB = int(info.Size() / 1024)
		}
		voices = append(voices, voice)
	}
	sort.SliceStable(voices, func(i, j int) bool {
		return voices[i].Filename < voices[j].Filename
	})
	return voices, nil
}

// parsePiperVoiceFilename decodes "<lang>_<REGION>-<name>-<quality>.onnx"
// into a PiperVoiceInfo. Best-effort: any deviation from the convention
// returns a PiperVoiceInfo with only Filename populated so the UI still
// lists every .onnx file the operator has installed.
func parsePiperVoiceFilename(name string) PiperVoiceInfo {
	out := PiperVoiceInfo{Filename: name}
	base := strings.TrimSuffix(name, ".onnx")
	// Pattern: lang_REGION-rest. Some files (custom) may omit region —
	// e.g. "voice-en.onnx" — in which case we cannot derive locale.
	dashParts := strings.SplitN(base, "-", 3)
	if len(dashParts) == 0 {
		return out
	}
	head := dashParts[0]
	if i := strings.Index(head, "_"); i > 0 {
		out.Locale = strings.ToLower(head[:i])
		out.Region = strings.ToUpper(head[i+1:])
	} else if len(head) == 2 {
		// No region; treat the whole head as locale only when it looks
		// like a 2-letter language code.
		out.Locale = strings.ToLower(head)
	}
	if len(dashParts) >= 2 {
		out.Name = dashParts[1]
	}
	if len(dashParts) >= 3 {
		out.Quality = dashParts[2]
	}
	return out
}

// Probe runs piper with a short test phrase to verify the binary and
// the requested voice work end-to-end. Returns the synthesized WAV
// bytes (small — a few seconds of speech) on success. Used by the
// Settings UI to preview a voice without persisting any text.
func (p *Piper) Probe(ctx context.Context, voice, locale string) (*Result, error) {
	if p == nil {
		return nil, errors.New("piper: nil provider")
	}
	phrase := "Hello from piper."
	if normaliseLocaleShort(locale) == "de" {
		phrase = "Hallo, hier spricht Piper."
	}
	return p.Synthesize(ctx, phrase, SynthesizeOpts{Voice: voice, Locale: locale})
}

// estimateWAVDuration derives playback duration from the WAV data
// chunk + sample rate. Returns 0 when the bytes count is below the
// header size.
func estimateWAVDuration(audio []byte, sampleRate int) time.Duration {
	if len(audio) < 44 || sampleRate <= 0 {
		return 0
	}
	dataBytes := len(audio) - 44
	// 16-bit mono PCM at the given sample rate: bytes / (2 * rate) seconds.
	seconds := float64(dataBytes) / float64(2*sampleRate)
	return time.Duration(seconds * float64(time.Second))
}

// Package dictation provides an embeddable strict Dictation runtime.
//
// Dictation is the most boundary-strict of the three SpeechKit modes: STT
// turns speech into text and the pipeline stops there. No LLM rewriting,
// no codewords, no tool invocation. Host apps integrate this when they
// want a transcript surface they can route into their own editor, command
// dispatcher, or downstream processor.
//
// Use [NewService] (or its original name [NewRuntime]) to construct an
// instance; pass a recorder, a [speechkit.Transcriber] — typically
// stt.AsTranscriber wrapped around any stt.STTProvider — and the
// [speechkit.RuntimePolicy] from the host config.
package dictation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

var (
	ErrMissingRecorder    = errors.New("speechkit dictation: recorder is required")
	ErrMissingTranscriber = errors.New("speechkit dictation: transcriber is required")
	ErrAlreadyRecording   = errors.New("speechkit dictation: already recording")
	ErrNotRecording       = errors.New("speechkit dictation: not recording")
	ErrAudioTooShort      = errors.New("speechkit dictation: audio too short")
)

// Options configures a Dictation Runtime. Recorder and Transcriber are
// required; NewRuntime returns ErrMissingRecorder / ErrMissingTranscriber
// otherwise. Everything else has a documented default.
type Options struct {
	// Recorder supplies the PCM audio for one dictation. audio/capture.Session
	// satisfies it; tests use any in-memory AudioRecorder.
	Recorder speechkit.AudioRecorder
	// Transcriber turns the recorded WAV into a Transcript. Wrap an
	// stt.STTProvider or stt.Router with stt.AsTranscriber.
	Transcriber speechkit.Transcriber
	// Output receives the final transcript. Nil means the transcript is only
	// returned from Stop / stored; the runtime does not inject text anywhere.
	Output speechkit.TranscriptOutput
	// Store optionally persists every completed dictation.
	Store speechkit.Persistence
	// Policy is the host's runtime policy (local-only, allowed providers, ...).
	// It is validated against Profiles at construction time.
	Policy speechkit.RuntimePolicy
	// Profiles is the provider catalog the Policy is checked against. Empty
	// means catalog.DefaultProviderProfiles().
	Profiles []speechkit.ProviderProfile
	// Language is the BCP-47 hint passed to the transcriber; "" means "auto".
	Language string
	// Target is an opaque, host-defined value handed unchanged to
	// Output.Deliver as its target argument. The runtime never inspects it; it
	// exists so a host can route one Output to several destinations (an editor
	// handle, a window id, a "clipboard" marker) without a per-target Output.
	// Nil is valid and means "the Output's default destination".
	Target any
	// MinPCMBytes is the shortest recording that is transcribed; shorter
	// recordings fail Stop with ErrAudioTooShort. <= 0 means
	// speechkit.DefaultMinPCMBytes.
	MinPCMBytes int
}

// Runtime is an embeddable Dictation-only service. It keeps the mode boundary
// strict: audio in, final text out, no tool calls or LLM rewriting.
type Runtime struct {
	recorder        speechkit.AudioRecorder
	transcriber     speechkit.Transcriber
	output          speechkit.TranscriptOutput
	store           speechkit.Persistence
	language        string
	target          any
	minPCMBytes     int
	providerProfile string

	mu        sync.Mutex
	recording bool
	startedAt time.Time
}

var _ speechkit.DictationService = (*Runtime)(nil)

// Service is the mode-service name for the Dictation runtime, matching
// assist.Service, tts.Service and voiceagent.Service so hosts wire the three
// modes with one vocabulary. Runtime and Service are the same type.
type Service = Runtime

// NewService constructs a Dictation service; it is NewRuntime under the name
// the other mode packages use.
func NewService(opts Options) (*Service, error) {
	return NewRuntime(opts)
}

func NewRuntime(opts Options) (*Runtime, error) {
	if opts.Recorder == nil {
		return nil, ErrMissingRecorder
	}
	if opts.Transcriber == nil {
		return nil, ErrMissingTranscriber
	}
	if opts.MinPCMBytes <= 0 {
		opts.MinPCMBytes = speechkit.DefaultMinPCMBytes
	}
	if opts.Language == "" {
		opts.Language = "auto"
	}

	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = catalog.DefaultProviderProfiles()
	}
	providerProfile, err := validateDictationPolicy(profiles, opts.Policy)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		recorder:        opts.Recorder,
		transcriber:     opts.Transcriber,
		output:          opts.Output,
		store:           opts.Store,
		language:        opts.Language,
		target:          opts.Target,
		minPCMBytes:     opts.MinPCMBytes,
		providerProfile: providerProfile,
	}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return ErrMissingRecorder
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	if r.recording {
		r.mu.Unlock()
		return ErrAlreadyRecording
	}
	r.recording = true
	r.startedAt = time.Now().UTC()
	r.mu.Unlock()

	if err := r.recorder.Start(); err != nil {
		r.mu.Lock()
		r.recording = false
		r.startedAt = time.Time{}
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *Runtime) Stop(ctx context.Context) (speechkit.DictationRun, error) {
	if r == nil {
		return speechkit.DictationRun{}, ErrMissingRecorder
	}
	if err := ctx.Err(); err != nil {
		return speechkit.DictationRun{}, err
	}

	r.mu.Lock()
	if !r.recording {
		r.mu.Unlock()
		return speechkit.DictationRun{}, ErrNotRecording
	}
	startedAt := r.startedAt
	r.recording = false
	r.startedAt = time.Time{}
	r.mu.Unlock()

	pcm, err := r.recorder.Stop()
	completedAt := time.Now().UTC()
	if err != nil {
		return speechkit.DictationRun{}, err
	}

	durationSecs := speechkit.PCMDurationSecs(pcm)
	run := speechkit.DictationRun{
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		ProviderProfile:  r.providerProfile,
		AudioDurationMs:  int64(durationSecs * 1000),
		ProcessingTimeMs: completedAt.Sub(startedAt).Milliseconds(),
	}
	if len(pcm) < r.minPCMBytes {
		return run, ErrAudioTooShort
	}

	transcribeStarted := time.Now()
	wav := speechkit.PCMToWAV(pcm)
	transcript, err := r.transcriber.Transcribe(ctx, wav, durationSecs, r.language)
	if err != nil {
		return run, err
	}
	if transcript.Duration == 0 {
		transcript.Duration = time.Since(transcribeStarted)
	}
	if transcript.Language == "" {
		transcript.Language = r.language
	}
	run.Transcript = transcript
	run.ProcessingTimeMs = time.Since(transcribeStarted).Milliseconds()

	if r.output != nil {
		if err := r.output.Deliver(ctx, transcript, r.target); err != nil {
			return run, err
		}
	}
	if r.store != nil {
		if err := r.store.SaveTranscription(ctx, transcript.Text, transcript.Language, transcript.Provider, transcript.Model, run.AudioDurationMs, run.ProcessingTimeMs, wav); err != nil {
			return run, fmt.Errorf("save transcription: %w", err)
		}
	}

	return run, nil
}

func validateDictationPolicy(profiles []speechkit.ProviderProfile, policy speechkit.RuntimePolicy) (string, error) {
	for _, mode := range policy.EnabledModes {
		if normalized := speechkit.NormalizeMode(mode); normalized != speechkit.ModeNone && normalized != speechkit.ModeDictation {
			return "", fmt.Errorf("speechkit dictation: policy enables non-dictation mode %q", normalized)
		}
	}
	for mode := range policy.FixedProfiles {
		if normalized := speechkit.NormalizeMode(mode); normalized != speechkit.ModeNone && normalized != speechkit.ModeDictation {
			return "", fmt.Errorf("speechkit dictation: fixed profile configured for non-dictation mode %q", normalized)
		}
	}
	if err := speechkit.ValidateRuntimePolicy(profiles, policy); err != nil {
		return "", err
	}

	filtered := speechkit.FilterProviderProfiles(profiles, speechkit.RuntimePolicy{
		EnabledModes:    []speechkit.Mode{speechkit.ModeDictation},
		AllowedProfiles: policy.AllowedProfiles,
		FixedProfiles:   policy.FixedProfiles,
		AllowFallbacks:  policy.AllowFallbacks,
		ModeBehaviors:   policy.ModeBehaviors,
	})
	if len(filtered) == 0 {
		return "", fmt.Errorf("speechkit dictation: policy leaves no usable dictation profiles")
	}
	for _, profile := range filtered {
		if profile.Default || profile.Recommended {
			return profile.ID, nil
		}
	}
	return filtered[0].ID, nil
}

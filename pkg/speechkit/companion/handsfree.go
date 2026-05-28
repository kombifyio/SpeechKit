// Package companion provides small composers for hands-free SpeechKit hosts.
package companion

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

var ErrMissingRuntime = errors.New("speechkit companion: runtime is required")
var ErrMissingAssist = errors.New("speechkit companion: assist service is required")
var ErrMissingContext = errors.New("speechkit companion: context is required")

// HandsFreeTarget identifies which strict SpeechKit mode a hands-free
// activation should start. Hands-free is a capability layer, not a fourth
// SpeechKit mode.
type HandsFreeTarget string

const (
	// TargetAssist runs a one-shot Assist request and may synthesize spoken
	// output. This is the Siri/Alexa-style Voice-Companion path.
	TargetAssist HandsFreeTarget = "assist"
	// TargetVoiceAgent starts a realtime Voice Agent session for continuous
	// dialogue such as companion or game-moderator flows.
	TargetVoiceAgent HandsFreeTarget = "voice_agent"
	// TargetDictationUIAssisted starts Dictation through the host command bus.
	// It intentionally does not synthesize audio: text output still belongs to
	// a visible target or explicit commit surface owned by the host UI.
	TargetDictationUIAssisted HandsFreeTarget = "dictation_ui_assisted"
)

// WakeRequestFunc converts a wake detection into an Assist request. Hosts use
// it to attach their transcript/capture result to the framework composer.
type WakeRequestFunc func(context.Context, wakeword.DetectionEvent) (speechkit.AssistRequest, bool)

type Options struct {
	Runtime     *speechkit.Runtime
	TargetMode  HandsFreeTarget
	WakeSink    wakeword.Sink
	WakeRequest WakeRequestFunc
	Assist      speechkit.AssistService
	VoiceAgent  speechkit.VoiceAgentService
	TTS         *tts.Service
}

type HandsFree struct {
	runtime            *speechkit.Runtime
	targetMode         HandsFreeTarget
	downstreamWakeSink wakeword.Sink
	wakeRequest        WakeRequestFunc
	assist             speechkit.AssistService
	voiceAgent         speechkit.VoiceAgentService
	tts                *tts.Service
}

func NewHandsFree(opts Options) (*HandsFree, error) {
	if opts.Runtime == nil {
		return nil, ErrMissingRuntime
	}
	var targetMode HandsFreeTarget
	if opts.TargetMode != "" {
		targetMode = normalizeHandsFreeTarget(string(opts.TargetMode))
	}
	return &HandsFree{
		runtime:            opts.Runtime,
		targetMode:         targetMode,
		downstreamWakeSink: opts.WakeSink,
		wakeRequest:        opts.WakeRequest,
		assist:             opts.Assist,
		voiceAgent:         opts.VoiceAgent,
		tts:                opts.TTS,
	}, nil
}

func (h *HandsFree) Runtime() *speechkit.Runtime {
	if h == nil {
		return nil
	}
	return h.runtime
}

func (h *HandsFree) Events() <-chan speechkit.Event {
	if h == nil || h.runtime == nil {
		return nil
	}
	return h.runtime.Events()
}

func (h *HandsFree) Start(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return ErrMissingRuntime
	}
	h.runtime.Publish(speechkit.Event{Type: speechkit.EventCompanionSessionStarted})
	return h.runtime.Start(ctx)
}

func (h *HandsFree) Stop(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return ErrMissingRuntime
	}
	err := h.runtime.Stop(ctx)
	h.runtime.Publish(speechkit.Event{Type: speechkit.EventCompanionSessionEnded})
	return err
}

func (h *HandsFree) WakeSink() wakeword.Sink {
	if h == nil {
		return nil
	}
	return wakeword.SinkFunc(func(ev wakeword.DetectionEvent) {
		if err := h.HandleWake(context.Background(), ev); err != nil {
			h.publishError(err)
		}
	})
}

// HandleWake publishes a wake event, forwards it to the optional downstream
// sink, and starts the configured hands-free target. Assist and Voice Agent can
// run without a visible UI; Dictation is UI-assisted and dispatches into the
// host command bus so the host can choose the text target and commit surface.
func (h *HandsFree) HandleWake(ctx context.Context, ev wakeword.DetectionEvent) error {
	if h == nil || h.runtime == nil {
		return ErrMissingRuntime
	}
	if ctx == nil {
		return ErrMissingContext
	}
	target := h.targetForWake(ev)
	h.runtime.Publish(speechkit.Event{
		Type:     speechkit.EventWakeFired,
		Mode:     string(target),
		Message:  ev.Phrase,
		Metadata: speechkit.NewMetadata(wakeMetadata(ev)),
	})
	if h.downstreamWakeSink != nil {
		h.downstreamWakeSink.Emit(ev)
	}

	switch target {
	case TargetVoiceAgent:
		if h.voiceAgent == nil {
			return nil
		}
		h.runtime.Publish(speechkit.Event{Type: speechkit.EventCompanionSessionStarted, Mode: "voice_agent"})
		return h.voiceAgent.Start(ctx)
	case TargetDictationUIAssisted:
		return h.runtime.Commands().Dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandStartMode,
			Text: ev.Phrase,
			Metadata: map[string]string{
				"mode":              "dictate",
				"source":            "hands_free",
				"hands_free_target": string(TargetDictationUIAssisted),
				"activation":        "wake",
			},
		})
	default:
		if h.wakeRequest == nil {
			return nil
		}
		if h.assist == nil {
			return ErrMissingAssist
		}
		req, ok := h.wakeRequest(ctx, ev)
		if !ok {
			return nil
		}
		_, err := h.ProcessAssist(ctx, req)
		return err
	}
}

func (h *HandsFree) targetForWake(ev wakeword.DetectionEvent) HandsFreeTarget {
	if h != nil && h.targetMode != "" {
		return h.targetMode
	}
	return normalizeHandsFreeTarget(ev.Mode)
}

// ProcessAssist runs a one-shot Assist request through the configured service
// and, when TTS is present, synthesizes the spoken response.
func (h *HandsFree) ProcessAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if h == nil || h.runtime == nil {
		return speechkit.AssistResult{}, ErrMissingRuntime
	}
	if h.assist == nil {
		return speechkit.AssistResult{}, ErrMissingAssist
	}
	if ctx == nil {
		return speechkit.AssistResult{}, ErrMissingContext
	}
	h.runtime.Publish(speechkit.Event{Type: speechkit.EventProcessingStarted, Mode: "assist", Text: req.Text})
	result, err := h.assist.Process(ctx, req)
	if err != nil {
		h.publishError(err)
		return result, err
	}
	h.runtime.Publish(speechkit.Event{
		Type: speechkit.EventSkillExecuted,
		Mode: "assist",
		Text: result.Text,
		Metadata: speechkit.NewMetadata(map[string]string{
			"action": result.Action,
			"kind":   result.Kind,
		}),
	})

	speak := strings.TrimSpace(result.SpeakText)
	if speak == "" {
		return result, nil
	}
	if h.tts == nil || result.Audio.Len() > 0 {
		return result, nil
	}
	h.runtime.Publish(speechkit.Event{Type: speechkit.EventTTSStarted, Mode: "assist", Text: speak})
	audio, err := h.tts.Synthesize(ctx, speak, tts.SynthesizeOpts{Locale: result.Locale})
	if err != nil {
		h.publishError(err)
		return result, fmt.Errorf("speechkit companion: synthesize assist response: %w", err)
	}
	result.Audio = speechkit.NewAudioData(audio.Audio)
	result.Format = audio.Format
	h.runtime.Publish(speechkit.Event{
		Type:     speechkit.EventTTSFinished,
		Mode:     "assist",
		Text:     speak,
		Provider: audio.Provider,
	})
	return result, nil
}

func (h *HandsFree) Assist() speechkit.AssistService {
	if h == nil {
		return nil
	}
	return h.assist
}

func (h *HandsFree) VoiceAgent() speechkit.VoiceAgentService {
	if h == nil {
		return nil
	}
	return h.voiceAgent
}

func (h *HandsFree) TTS() *tts.Service {
	if h == nil {
		return nil
	}
	return h.tts
}

func (h *HandsFree) publishError(err error) {
	if h != nil && h.runtime != nil && err != nil {
		h.runtime.Publish(speechkit.Event{Type: speechkit.EventErrorRaised, Err: err, Message: err.Error()})
	}
}

func normalizeHandsFreeTarget(mode string) HandsFreeTarget {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "voice-agent", "voiceagent", "voice_agent_hotkey":
		return TargetVoiceAgent
	case "voice_agent":
		return TargetVoiceAgent
	case "dictate", "dictation", "transcribe", "stt", "dictation_ui_assisted":
		return TargetDictationUIAssisted
	default:
		return TargetAssist
	}
}

func wakeMetadata(ev wakeword.DetectionEvent) map[string]string {
	meta := map[string]string{}
	if ev.Keyword != "" {
		meta["keyword"] = ev.Keyword
	}
	if ev.Probability != 0 {
		meta["probability"] = strconv.FormatFloat(float64(ev.Probability), 'f', -1, 32)
	}
	return meta
}

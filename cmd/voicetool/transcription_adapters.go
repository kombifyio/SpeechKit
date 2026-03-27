package main

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type routerTranscriber struct {
	router *router.Router
}

func (t routerTranscriber) Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	if t.router == nil {
		return speechkit.Transcript{}, fmt.Errorf("router not configured")
	}

	result, err := t.router.Route(ctx, audio, durationSecs, stt.TranscribeOpts{Language: language})
	if err != nil {
		return speechkit.Transcript{}, err
	}

	return speechkit.Transcript{
		Text:       result.Text,
		Language:   result.Language,
		Duration:   result.Duration,
		Provider:   result.Provider,
		Model:      result.Model,
		Confidence: result.Confidence,
	}, nil
}

type speechkitStoreAdapter struct {
	store store.Store
	state *appState
}

func (a speechkitStoreAdapter) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	if a.store == nil {
		return 0, nil
	}
	id, err := a.store.SaveQuickNote(ctx, text, language, provider, durationMs, latencyMs, audioData)
	if err != nil {
		return 0, err
	}
	if a.state != nil {
		a.state.publishSpeechKitEvent(speechkit.Event{
			Type:      speechkit.EventQuickNoteUpdated,
			Message:   "quick note updated",
			Text:      text,
			QuickNote: true,
		})
		a.state.addLog("Quick Note saved", "success")
	}
	return id, nil
}

func (a speechkitStoreAdapter) GetQuickNoteText(ctx context.Context, id int64) (string, error) {
	if a.store == nil {
		return "", nil
	}
	note, err := a.store.GetQuickNote(ctx, id)
	if err != nil {
		return "", err
	}
	return note.Text, nil
}

func (a speechkitStoreAdapter) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	if a.store == nil {
		return nil
	}
	if err := a.store.UpdateQuickNote(ctx, id, text); err != nil {
		return err
	}
	if a.state != nil {
		a.state.publishSpeechKitEvent(speechkit.Event{
			Type:      speechkit.EventQuickNoteUpdated,
			Message:   "quick note updated",
			Text:      text,
			QuickNote: true,
		})
		a.state.addLog(fmt.Sprintf("Quick Note #%d updated", id), "success")
	}
	return nil
}

func (a speechkitStoreAdapter) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	if a.store == nil {
		return nil
	}
	if err := a.store.UpdateQuickNoteCapture(ctx, id, text, provider, durationMs, latencyMs, audioData); err != nil {
		return err
	}
	if a.state != nil {
		a.state.publishSpeechKitEvent(speechkit.Event{
			Type:      speechkit.EventQuickNoteUpdated,
			Message:   "quick note updated",
			Text:      text,
			QuickNote: true,
		})
		a.state.addLog(fmt.Sprintf("Quick Note #%d updated", id), "success")
	}
	return nil
}

func (a speechkitStoreAdapter) SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	if a.store == nil {
		return nil
	}
	if err := a.store.SaveTranscription(ctx, text, language, provider, model, durationMs, latencyMs, audioData); err != nil {
		return err
	}
	if a.state != nil {
		a.state.incrementTranscriptions()
	}
	return nil
}

type desktopTranscriptOutput struct {
	handler     output.OutputHandler
	interceptor transcriptInterceptor
}

func (o desktopTranscriptOutput) Deliver(ctx context.Context, transcript speechkit.Transcript, target any) error {
	if o.interceptor != nil {
		handled, err := o.interceptor.Intercept(ctx, transcript, target)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}

	if o.handler == nil {
		return nil
	}

	outputTarget, _ := target.(output.Target)
	return o.handler.Handle(ctx, &stt.Result{
		Text:       transcript.Text,
		Language:   transcript.Language,
		Duration:   transcript.Duration,
		Provider:   transcript.Provider,
		Model:      transcript.Model,
		Confidence: transcript.Confidence,
	}, outputTarget)
}

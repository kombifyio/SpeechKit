// Package assist implements the Assist Mode pipeline:
// STT transcript → Codeword check → LLM → TTS → Result with both text and audio.
package assist

import (
	"context"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/firebase/genkit/go/core"
)

// Result is the framework output for Assist Mode.
// Always contains Text. Contains Audio when TTS is enabled.
type Result struct {
	Text      string // Full response text (always present)
	SpeakText string // TTS-optimized text
	Audio     []byte // TTS audio bytes (present when TTS enabled)
	Format    string // Audio format ("mp3", "wav", etc.)
	Action    string // "respond", "execute", "silent", "shortcut"
	Locale    string // Response language
	Shortcut  string // Matched shortcut intent, if any
}

// Pipeline orchestrates the Assist Mode flow.
type Pipeline struct {
	assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	ttsRouter  *tts.Router
	ttsEnabled bool
}

// NewPipeline creates an Assist Pipeline.
func NewPipeline(assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}], ttsRouter *tts.Router, ttsEnabled bool) *Pipeline {
	return &Pipeline{
		assistFlow: assistFlow,
		ttsRouter:  ttsRouter,
		ttsEnabled: ttsEnabled,
	}
}

// Process takes a transcript and produces a Result with text and optional audio.
func (p *Pipeline) Process(ctx context.Context, transcript string, opts ProcessOpts) (*Result, error) {
	if transcript == "" {
		return nil, fmt.Errorf("assist: empty transcript")
	}

	// Step 1: Check for codeword/shortcut match.
	resolution := shortcuts.Resolve(transcript)
	if resolution.Intent != "" {
		return p.handleShortcut(ctx, resolution, opts)
	}

	// Step 2: No shortcut match — send to LLM.
	return p.handleLLM(ctx, transcript, opts)
}

// ProcessOpts configures a single Assist request.
type ProcessOpts struct {
	Locale    string // "de", "en", etc.
	Selection string // Currently selected text
	Context   string // Additional context
}

func (p *Pipeline) handleShortcut(ctx context.Context, res shortcuts.Resolution, opts ProcessOpts) (*Result, error) {
	result := &Result{
		Action:   "shortcut",
		Shortcut: string(res.Intent),
		Locale:   opts.Locale,
	}

	// Generate response text based on shortcut type.
	switch res.Intent {
	case shortcuts.IntentCopyLast:
		result.Text = "Copied to clipboard."
		if opts.Locale == "de" || opts.Locale == "de-DE" {
			result.Text = "In die Zwischenablage kopiert."
		}
	case shortcuts.IntentInsertLast:
		result.Text = "Inserted."
		if opts.Locale == "de" || opts.Locale == "de-DE" {
			result.Text = "Eingefuegt."
		}
	case shortcuts.IntentSummarize:
		result.Text = "Summarizing..."
		if opts.Locale == "de" || opts.Locale == "de-DE" {
			result.Text = "Wird zusammengefasst..."
		}
	default:
		result.Text = "Command recognized."
	}

	result.SpeakText = result.Text

	// Synthesize TTS for the response.
	if err := p.synthesize(ctx, result); err != nil {
		log.Printf("assist: TTS for shortcut failed (non-fatal): %v", err)
	}

	return result, nil
}

func (p *Pipeline) handleLLM(ctx context.Context, transcript string, opts ProcessOpts) (*Result, error) {
	if p.assistFlow == nil {
		return nil, fmt.Errorf("assist: no LLM flow configured")
	}

	output, err := p.assistFlow.Run(ctx, flows.AssistInput{
		Utterance: transcript,
		Locale:    opts.Locale,
		Selection: opts.Selection,
		Context:   opts.Context,
	})
	if err != nil {
		return nil, fmt.Errorf("assist: LLM failed: %w", err)
	}

	result := &Result{
		Text:      output.Text,
		SpeakText: output.SpeakText,
		Action:    output.Action,
		Locale:    output.Locale,
	}

	// Synthesize TTS for the LLM response.
	if err := p.synthesize(ctx, result); err != nil {
		log.Printf("assist: TTS failed (non-fatal): %v", err)
	}

	return result, nil
}

// synthesize adds TTS audio to the result if TTS is enabled.
func (p *Pipeline) synthesize(ctx context.Context, result *Result) error {
	if !p.ttsEnabled || p.ttsRouter == nil {
		return nil
	}

	text := result.SpeakText
	if text == "" {
		text = result.Text
	}
	if text == "" || result.Action == "silent" {
		return nil
	}

	ttsResult, err := p.ttsRouter.Synthesize(ctx, text, tts.SynthesizeOpts{
		Locale: result.Locale,
	})
	if err != nil {
		return err
	}

	result.Audio = ttsResult.Audio
	result.Format = ttsResult.Format
	return nil
}

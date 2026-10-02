package telemetry

// dictation_outcome.go turns the framework's finalization vocabulary into the
// one judgement alerting needs: did the user keep their words?
//
// pkg/speechkit already separates recognition, output and history into named
// states, and refuses to pretend. What it deliberately does not do is decide
// how bad a combination is — that is a product judgement, and the public
// framework should not carry it. So the judgement lives here, on the host
// side, next to the exporter that would act on it.

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// DictationOutcomeName is the single event name for a finished dictation. One
// name plus a severity attribute beats three names, because an alert rule can
// then say "rate of lost over total" without enumerating outcomes.
const DictationOutcomeName = "speechkit.dictation.finalized"

// Attribute keys carrying the three finalization states verbatim, so a triage
// session can see which stage failed without re-deriving it from the severity.
const (
	AttrDictationRecognition = "speechkit.dictation.recognition"
	AttrDictationOutput      = "speechkit.dictation.output"
	AttrDictationPersistence = "speechkit.dictation.persistence"
	// AttrMode separates dictation from assist, which share one transcription
	// worker. Losing words matters in both, and a single alert rule should be
	// able to say which surface it happened on.
	AttrMode = "speechkit.mode"
)

// DictationOutcomeTerminal reports whether f can still change.
//
// An observer is called several times for one dictation — recognition, then
// the output result, then an optional history update — and reporting each call
// would multiply one dictation into three outcomes and make any rate wrong. It
// is terminal when no later callback can arrive:
//
//   - recognition failed: there is no text, so neither output nor history runs;
//   - history settled (saved or failed): history is the last step;
//   - history was never requested and output has settled: nothing follows.
//
// Recognition being empty is terminal for the same reason as a failure when
// nothing else was requested, and is handled by the last clause.
func DictationOutcomeTerminal(f speechkit.TranscriptionFinalization) bool {
	if f.Recognition == speechkit.RecognitionFailed {
		return true
	}
	switch f.Persistence {
	case speechkit.PersistenceSaved, speechkit.PersistenceFailed:
		return true
	case speechkit.PersistencePending:
		return false
	}
	// PersistenceNotRequested: the output stage is the last one that can move.
	switch f.Output {
	case speechkit.OutputRequested:
		return false
	default:
		return true
	}
}

// ClassifyDictationOutcome grades a terminal finalization.
//
// The line between degraded and lost is whether the words still exist anywhere
// the user can reach. Text that was submitted to the target application, or
// saved to history, is recoverable — the overlay offers copy and retry against
// exactly that. Text that reached neither is gone, and that is the only case
// worth waking someone for.
//
// A recognition failure counts as lost rather than degraded: the user spoke and
// there is nothing to recover, which is indistinguishable to them from losing
// text that existed. An empty recognition does not, because the most common
// cause is that nothing was said, and paging on silence would train the alert
// to be ignored.
func ClassifyDictationOutcome(f speechkit.TranscriptionFinalization) OutcomeSeverity {
	switch f.Recognition {
	case speechkit.RecognitionFailed:
		return OutcomeLost
	case speechkit.RecognitionEmpty:
		return OutcomeDegraded
	}

	delivered := f.Output == speechkit.OutputSubmitted
	saved := f.Persistence == speechkit.PersistenceSaved
	// Output that was never requested is not a failure to deliver: the caller
	// asked for transcription only, so history alone is the whole contract.
	outputFailed := f.Output == speechkit.OutputBlocked || f.Output == speechkit.OutputFailed
	persistenceFailed := f.Persistence == speechkit.PersistenceFailed

	switch {
	case delivered && (saved || f.Persistence == speechkit.PersistenceNotRequested):
		return OutcomeOK
	case f.Output == speechkit.OutputNotRequested && saved:
		return OutcomeOK
	case delivered || saved:
		// One half worked, so the words survive somewhere.
		return OutcomeDegraded
	case outputFailed || persistenceFailed:
		return OutcomeLost
	default:
		// Recognized, and nothing was asked of either stage. The caller has the
		// transcript in hand; nothing was lost.
		return OutcomeOK
	}
}

// ReportDictationOutcome grades f and records it, once, when it is terminal.
// Non-terminal callbacks are ignored rather than being the caller's problem, so
// a host can hand every finalization to it without tracking stages itself.
//
// The parameter list is closed on purpose. It takes no variadic attributes,
// because that is exactly the hole through which transcript text would
// eventually reach an exporter: everything recorded here is a closed
// enumeration the caller cannot widen.
func ReportDictationOutcome(ctx context.Context, mode string, f speechkit.TranscriptionFinalization) {
	if !DictationOutcomeTerminal(f) {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(AttrDictationRecognition, string(f.Recognition)),
		attribute.String(AttrDictationOutput, string(f.Output)),
		attribute.String(AttrDictationPersistence, string(f.Persistence)),
	}
	if mode != "" {
		attrs = append(attrs, attribute.String(AttrMode, mode))
	}
	ReportOutcome(ctx, DictationOutcomeName, ClassifyDictationOutcome(f), attrs...)
}

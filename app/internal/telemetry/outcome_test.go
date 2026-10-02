package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// recordOutcomes installs a recording provider for one test and restores the
// previous global afterwards, so tests stay independent of each other and of
// whatever ConfigureTracing may have installed.
func recordOutcomes(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

// The local-only guarantee in AGENTS.md is the reason this is the first test:
// a fresh install configures no endpoint, so ConfigureTracing installs nothing
// and reporting an outcome must reach nobody and cost nothing.
func TestReportOutcomeEmitsNothingWithoutAConfiguredEndpoint(t *testing.T) {
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	otel.SetTracerProvider(noop.NewTracerProvider())

	shutdown, err := ConfigureTracing(context.Background(), TracingOptions{})
	if err != nil {
		t.Fatalf("ConfigureTracing with no endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("ConfigureTracing returned a nil shutdown")
	}
	// The point is that this does not panic and installs no exporter; there is
	// nothing to assert on the wire precisely because nothing goes on it.
	ReportOutcome(context.Background(), "speechkit.test", OutcomeLost)
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestReportOutcomeOpensItsOwnSpanWhenThereIsNoActiveOne(t *testing.T) {
	recorder := recordOutcomes(t)

	ReportOutcome(context.Background(), "speechkit.test.outcome", OutcomeLost)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1 (a dictation finalizes outside any request)", len(spans))
	}
	span := spans[0]
	if span.Name() != "speechkit.test.outcome" {
		t.Errorf("span name = %q, want the outcome name", span.Name())
	}
	if span.Status().Code.String() != "Error" {
		t.Errorf("status = %s, want Error so a backend's error view and alert rules see it", span.Status().Code)
	}
	if len(span.Events()) != 1 || span.Events()[0].Name != "speechkit.test.outcome" {
		t.Errorf("events = %+v, want one named after the outcome", span.Events())
	}
	attrs := map[string]string{}
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs[AttrOutcomeSeverity] != string(OutcomeLost) {
		t.Errorf("severity attribute = %q, want %q", attrs[AttrOutcomeSeverity], OutcomeLost)
	}
	if attrs[AttrOutcomeName] != "speechkit.test.outcome" {
		t.Errorf("name attribute = %q", attrs[AttrOutcomeName])
	}
}

func TestReportOutcomeAttachesToAnActiveSpanInsteadOfOpeningAnother(t *testing.T) {
	recorder := recordOutcomes(t)

	ctx, parent := otel.Tracer("test").Start(context.Background(), "serving-a-request")
	ReportOutcome(ctx, "speechkit.test.outcome", OutcomeDegraded)
	parent.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1: an outcome inside a request stays on that request", len(spans))
	}
	if spans[0].Name() != "serving-a-request" {
		t.Fatalf("span name = %q, want the caller's span", spans[0].Name())
	}
	if len(spans[0].Events()) != 1 {
		t.Fatalf("events on the request span = %d, want the outcome", len(spans[0].Events()))
	}
	if spans[0].Status().Code.String() == "Error" {
		t.Error("a degraded outcome must not mark the whole request as failed")
	}
}

func TestReportOutcomeIgnoresAnUnnamedOutcome(t *testing.T) {
	recorder := recordOutcomes(t)
	ReportOutcome(context.Background(), "", OutcomeLost)
	if len(recorder.Ended()) != 0 {
		t.Fatal("an outcome with no name is a caller bug, not a span nobody can query")
	}
}

func TestDictationOutcomeTerminal(t *testing.T) {
	f := func(r speechkit.RecognitionState, o speechkit.OutputState, p speechkit.PersistenceState) speechkit.TranscriptionFinalization {
		return speechkit.TranscriptionFinalization{Recognition: r, Output: o, Persistence: p}
	}
	cases := []struct {
		name string
		in   speechkit.TranscriptionFinalization
		want bool
	}{
		{"recognition failed ends it immediately", f(speechkit.RecognitionFailed, speechkit.OutputNotRequested, speechkit.PersistenceNotRequested), true},
		{"history still pending", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistencePending), false},
		{"history saved", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistenceSaved), true},
		{"history failed", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistenceFailed), true},
		{"output still awaited, no history wanted", f(speechkit.RecognitionRecognized, speechkit.OutputRequested, speechkit.PersistenceNotRequested), false},
		{"output settled, no history wanted", f(speechkit.RecognitionRecognized, speechkit.OutputBlocked, speechkit.PersistenceNotRequested), true},
		{"empty recognition asks nothing of either stage", f(speechkit.RecognitionEmpty, speechkit.OutputNotRequested, speechkit.PersistenceNotRequested), true},
		{"the very first callback, before anything ran", f(speechkit.RecognitionRecognized, speechkit.OutputRequested, speechkit.PersistencePending), false},
	}
	for _, tc := range cases {
		if got := DictationOutcomeTerminal(tc.in); got != tc.want {
			t.Errorf("%s: terminal = %v, want %v (%+v)", tc.name, got, tc.want, tc.in)
		}
	}
}

func TestClassifyDictationOutcome(t *testing.T) {
	f := func(r speechkit.RecognitionState, o speechkit.OutputState, p speechkit.PersistenceState) speechkit.TranscriptionFinalization {
		return speechkit.TranscriptionFinalization{Recognition: r, Output: o, Persistence: p}
	}
	cases := []struct {
		name string
		in   speechkit.TranscriptionFinalization
		want OutcomeSeverity
	}{
		{"delivered and saved", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistenceSaved), OutcomeOK},
		{"delivered, history not wanted", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistenceNotRequested), OutcomeOK},
		{"transcribe-only run that saved", f(speechkit.RecognitionRecognized, speechkit.OutputNotRequested, speechkit.PersistenceSaved), OutcomeOK},

		// One half worked: the overlay's copy and retry act on exactly this.
		{"blocked but saved", f(speechkit.RecognitionRecognized, speechkit.OutputBlocked, speechkit.PersistenceSaved), OutcomeDegraded},
		{"delivered but history failed", f(speechkit.RecognitionRecognized, speechkit.OutputSubmitted, speechkit.PersistenceFailed), OutcomeDegraded},
		{"heard nothing", f(speechkit.RecognitionEmpty, speechkit.OutputNotRequested, speechkit.PersistenceNotRequested), OutcomeDegraded},

		// Nothing survives: this is what should page.
		{"blocked and history failed", f(speechkit.RecognitionRecognized, speechkit.OutputBlocked, speechkit.PersistenceFailed), OutcomeLost},
		{"output failed, history not wanted", f(speechkit.RecognitionRecognized, speechkit.OutputFailed, speechkit.PersistenceNotRequested), OutcomeLost},
		{"recognition failed", f(speechkit.RecognitionFailed, speechkit.OutputNotRequested, speechkit.PersistenceNotRequested), OutcomeLost},
	}
	for _, tc := range cases {
		if got := ClassifyDictationOutcome(tc.in); got != tc.want {
			t.Errorf("%s: severity = %q, want %q (%+v)", tc.name, got, tc.want, tc.in)
		}
	}
}

func TestReportDictationOutcomeSkipsNonTerminalCallbacks(t *testing.T) {
	recorder := recordOutcomes(t)

	// The three callbacks one dictation produces. Only the last is terminal, so
	// exactly one outcome must reach the backend — otherwise every rate built
	// on it counts one dictation three times.
	ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
		Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputRequested, Persistence: speechkit.PersistencePending,
	})
	ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
		Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputSubmitted, Persistence: speechkit.PersistencePending,
	})
	ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
		Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputSubmitted, Persistence: speechkit.PersistenceSaved,
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("outcomes recorded = %d, want exactly 1 for one dictation", len(spans))
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs[AttrOutcomeSeverity] != string(OutcomeOK) {
		t.Errorf("severity = %q, want ok", attrs[AttrOutcomeSeverity])
	}
	if attrs[AttrDictationPersistence] != string(speechkit.PersistenceSaved) {
		t.Errorf("persistence attribute = %q", attrs[AttrDictationPersistence])
	}
}

// The privacy invariant is binding on this path because it leaves the machine
// by definition. Assert on the whole attribute set rather than on a denylist,
// so a future attribute has to be added here consciously.
func TestReportDictationOutcomeCarriesNoContent(t *testing.T) {
	recorder := recordOutcomes(t)
	ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
		ID: 42, Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputBlocked, Persistence: speechkit.PersistenceFailed,
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	allowed := map[string]bool{
		AttrOutcomeName: true, AttrOutcomeSeverity: true,
		AttrDictationRecognition: true, AttrDictationOutput: true, AttrDictationPersistence: true,
		AttrMode: true,
	}
	for _, kv := range spans[0].Attributes() {
		if !allowed[string(kv.Key)] {
			t.Errorf("unexpected attribute %q on an exported outcome; content must never leave", kv.Key)
		}
	}
}

// The reason KeepLostOutcomes exists: production samples at 0.2, which is
// right for ordinary traffic and would silently drop four out of five losses.
// A rate is only worth alerting on if the numerator cannot be sampled away.
func TestKeepLostOutcomesSurvivesALowSamplingRatio(t *testing.T) {
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		// Deliberately harsher than production: nothing ordinary survives.
		sdktrace.WithSampler(KeepLostOutcomes(sdktrace.NeverSample())),
	))

	for range 20 {
		ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
			Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputSubmitted, Persistence: speechkit.PersistenceSaved,
		})
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ok outcomes exported = %d, want 0 under NeverSample: ordinary traffic must still obey the ratio", got)
	}

	for range 20 {
		ReportDictationOutcome(context.Background(), "dictate", speechkit.TranscriptionFinalization{
			Recognition: speechkit.RecognitionRecognized, Output: speechkit.OutputBlocked, Persistence: speechkit.PersistenceFailed,
		})
	}
	if got := len(recorder.Ended()); got != 20 {
		t.Fatalf("lost outcomes exported = %d, want all 20: a loss must never be sampled away", got)
	}
}

func TestKeepLostOutcomesDescribesWhatItWraps(t *testing.T) {
	got := KeepLostOutcomes(sdktrace.NeverSample()).Description()
	if got != "KeepLostOutcomes{AlwaysOffSampler}" {
		t.Errorf("Description() = %q; a sampler that cannot be identified in a config dump is a debugging trap", got)
	}
}

package skills_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// fakeFallback stands in for a host's text-utility executor (copy_last,
// insert_last, summarize). It records the last call and returns a canned
// result, mirroring the mock the private pipeline tests used.
type fakeFallback struct {
	calls  int
	call   assist.ToolCall
	result assist.ToolResult
	err    error
}

func (f *fakeFallback) ExecuteTool(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	f.calls++
	f.call = call
	return f.result, f.err
}

type fakeTTS struct {
	opts tts.SynthesizeOpts
}

func (f *fakeTTS) Synthesize(_ context.Context, _ string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	f.opts = opts
	return &tts.Result{Audio: []byte("audio"), Format: "mp3", Provider: "fake"}, nil
}

func countingGenerator(calls *int, text string) assist.Generator {
	return assist.GenerateFunc(func(_ context.Context, _ speechkit.AssistRequest) (speechkit.AssistResult, error) {
		*calls++
		return speechkit.AssistResult{Text: text, SpeakText: text, Action: "respond", Locale: "en", Surface: speechkit.AssistSurfacePanel}, nil
	})
}

func TestSmartHomeIntentNeverFallsThroughToLLMWhenHAIsUnavailable(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		locale     string
		wantLocale string
	}{
		{name: "English", transcript: "turn on the kitchen light", locale: "en-US", wantLocale: "en"},
		{name: "German", transcript: "schalte das Küchenlicht ein", locale: "de-DE", wantLocale: "de"},
		{name: "Spanish", transcript: "enciende la luz de la cocina", locale: "es-MX", wantLocale: "es"},
		{name: "Simplified Chinese", transcript: "家庭助理打开客厅灯", locale: "zh-Hans-CN", wantLocale: "zh-Hans"},
		{name: "Hindi", transcript: "लाइट चालू करो", locale: "hi-IN", wantLocale: "hi"},
		{name: "Arabic", transcript: "شغّل الضوء في المطبخ", locale: "ar-EG", wantLocale: "ar"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cat := skills.New(skills.Options{}) // no Home Assistant bridge configured
			speaker := &fakeTTS{}
			generatorCalls := 0
			svc, err := assist.NewService(assist.Options{
				Matcher:    cat.Matcher(),
				Executor:   cat.Executor(),
				Generator:  countingGenerator(&generatorCalls, "unsafe general model response"),
				TTSRouter:  speaker,
				TTSEnabled: true,
			})
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}

			result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: tc.transcript, Locale: tc.locale})
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if generatorCalls != 0 || result.Text == "unsafe general model response" {
				t.Fatal("recognized smart-home request reached the general Assist model")
			}
			if result.Action == "silent" || result.Surface == speechkit.AssistSurfaceSilent || result.Text == "" {
				t.Fatalf("smart-home denial must be terminal: %#v", result)
			}
			if got, want := result.ShortcutID, string(shortcuts.IntentHomeAssistant); got != want {
				t.Fatalf("ShortcutID = %q, want %q", got, want)
			}
			if result.MessageID != localization.CompanionHomeAssistantNotConfigured || result.ReasonCode != "not_configured" {
				t.Fatalf("smart-home denial metadata = %q/%q", result.MessageID, result.ReasonCode)
			}
			if result.Locale != tc.wantLocale || speaker.opts.Locale != tc.wantLocale {
				t.Fatalf("result/TTS locale = %q/%q, want %q", result.Locale, speaker.opts.Locale, tc.wantLocale)
			}
			if want := localization.Text(tc.wantLocale, result.MessageID); result.Text != want {
				t.Fatalf("result text = %q, want %q", result.Text, want)
			}
			if result.Audio.Len() == 0 {
				t.Fatal("the localized denial should still be spoken when TTS is enabled")
			}
		})
	}
}

// TestSilentToolResultFallsThroughToLLM covers the Voice-Companion
// fallthrough contract: when a matched executor returns an empty silent
// result (the documented "I can't answer this specific payload — defer to
// the LLM" signal), the service re-routes to the Generator instead of
// returning silence. Home Assistant never emits a silent result because
// smart-home requests are fail-closed.
func TestSilentToolResultFallsThroughToLLM(t *testing.T) {
	fallback := &fakeFallback{result: assist.ToolResult{Action: "silent", Surface: speechkit.AssistSurfaceSilent, Locale: "en"}}
	cat := skills.New(skills.Options{Fallback: fallback})
	generatorCalls := 0
	svc, err := assist.NewService(assist.Options{
		Matcher:   cat.Matcher(),
		Executor:  cat.Executor(),
		Generator: countingGenerator(&generatorCalls, "LLM picked it up"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// "summarize this" is a registered utility handled by the host fallback.
	result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "summarize this", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallback.calls)
	}
	if generatorCalls != 1 || result.Text != "LLM picked it up" || result.Action != "respond" {
		t.Fatalf("result = %#v (generator calls %d), want the LLM fallthrough", result, generatorCalls)
	}
}

// TestSilentToolResultWithoutLLMReturnsSilent guards the inverse: when no
// Generator is configured, the silent result is honoured rather than
// dropped, preserving the QuickNote-style "silent" behaviour for hosts that
// intentionally have no LLM provider.
func TestSilentToolResultWithoutLLMReturnsSilent(t *testing.T) {
	fallback := &fakeFallback{result: assist.ToolResult{Action: "silent", Surface: speechkit.AssistSurfaceSilent, Locale: "en"}}
	cat := skills.New(skills.Options{Fallback: fallback})
	svc, err := assist.NewService(assist.Options{Matcher: cat.Matcher(), Executor: cat.Executor()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "summarize this", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.Action != "silent" || result.Surface != speechkit.AssistSurfaceSilent {
		t.Fatalf("result = %#v, want the silent result kept without an LLM", result)
	}
}

// TestSilentToolResultWithTextStaysSilent covers a host returning a silent
// result WITH explanatory text ("Already saved silently"). That is an
// explicit quiet acknowledgement, not a request for the model.
func TestSilentToolResultWithTextStaysSilent(t *testing.T) {
	fallback := &fakeFallback{result: assist.ToolResult{
		Action:  "silent",
		Surface: speechkit.AssistSurfaceSilent,
		Text:    "Already saved silently",
		Locale:  "en",
	}}
	cat := skills.New(skills.Options{Fallback: fallback})
	generatorCalls := 0
	svc, err := assist.NewService(assist.Options{
		Matcher:   cat.Matcher(),
		Executor:  cat.Executor(),
		Generator: countingGenerator(&generatorCalls, "LLM fallback"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "summarize this", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if generatorCalls != 0 || result.Text != "Already saved silently" {
		t.Fatalf("result = %#v (generator calls %d), want the host-provided silent text", result, generatorCalls)
	}
}

func TestCanHandleLocallyRejectsModelRequiredUtilities(t *testing.T) {
	cat := skills.New(skills.Options{Fallback: &fakeFallback{}})
	if cat.CanHandleLocally(speechkit.AssistRequest{Text: "summarize this", Locale: "en"}) {
		t.Fatal("CanHandleLocally returned true for the model-required summarize utility")
	}
	if !cat.CanHandleLocally(speechkit.AssistRequest{Text: "copy last", Locale: "en"}) {
		t.Fatal("CanHandleLocally returned false for the action-only copy utility")
	}
	if !cat.CanHandleLocally(speechkit.AssistRequest{Text: "what time is it", Locale: "en"}) {
		t.Fatal("CanHandleLocally returned false for a built-in skill")
	}
	if cat.CanHandleLocally(speechkit.AssistRequest{Text: "tell me a long story about dragons", Locale: "en"}) {
		t.Fatal("CanHandleLocally returned true for free-form text")
	}
	var none *skills.Catalog
	if none.CanHandleLocally(speechkit.AssistRequest{Text: "copy last"}) {
		t.Fatal("nil catalog must report false")
	}
}

func TestMatchedCallCarriesPayloadSelectionTargetAndTranscript(t *testing.T) {
	fallback := &fakeFallback{result: assist.ToolResult{
		Text:      "Kurzfassung",
		SpeakText: "Kurzfassung",
		Action:    "execute",
		Locale:    "de",
		Surface:   speechkit.AssistSurfacePanel,
		Kind:      assist.KindWorkProduct,
	}}
	cat := skills.New(skills.Options{Fallback: fallback})
	generatorCalls := 0
	svc, err := assist.NewService(assist.Options{
		Matcher:   cat.Matcher(),
		Executor:  cat.Executor(),
		Generator: countingGenerator(&generatorCalls, "sollte nicht verwendet werden"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := svc.Process(context.Background(), speechkit.AssistRequest{
		Text:      "zusammenfassen in drei punkten",
		Locale:    "de",
		Selection: "Der markierte Text",
		Context:   "Active application: Code",
		Target:    "target-window",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if generatorCalls != 0 || fallback.calls != 1 {
		t.Fatalf("generator calls = %d fallback calls = %d, want the utility to win", generatorCalls, fallback.calls)
	}
	call := fallback.call
	if call.Intent != string(shortcuts.IntentSummarize) || call.Payload != "in drei punkten" {
		t.Fatalf("call = %#v, want summarize with payload \"in drei punkten\"", call)
	}
	if call.Selection != "Der markierte Text" || call.Target != "target-window" || call.Context != "Active application: Code" {
		t.Fatalf("call = %#v, want selection, target and context forwarded", call)
	}
	if call.Transcript != "zusammenfassen in drei punkten" {
		t.Fatalf("Transcript = %q, want the full utterance", call.Transcript)
	}
	if result.Text != "Kurzfassung" || result.Action != "execute" || result.Surface != speechkit.AssistSurfacePanel || result.Kind != assist.KindWorkProduct {
		t.Fatalf("result = %#v", result)
	}
	if result.ShortcutID != "summarize" {
		t.Fatalf("ShortcutID = %q, want summarize", result.ShortcutID)
	}
}

func TestFallbackResultsReceiveRegistryDefaults(t *testing.T) {
	fallback := &fakeFallback{}
	cat := skills.New(skills.Options{Fallback: fallback})
	svc, err := assist.NewService(assist.Options{Matcher: cat.Matcher(), Executor: cat.Executor()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	for _, tc := range []struct {
		text    string
		surface speechkit.AssistSurfaceDecision
		kind    string
	}{
		{text: "copy last", surface: speechkit.AssistSurfaceActionAck, kind: assist.KindUtilityAction},
		{text: "insert last", surface: speechkit.AssistSurfaceActionAck, kind: assist.KindUtilityAction},
		{text: "summarize this", surface: speechkit.AssistSurfacePanel, kind: assist.KindWorkProduct},
	} {
		fallback.result = assist.ToolResult{Text: "done: " + tc.text}
		result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: tc.text, Locale: "en"})
		if err != nil {
			t.Fatalf("Process(%q): %v", tc.text, err)
		}
		if result.Surface != tc.surface || result.Kind != tc.kind {
			t.Fatalf("%q surface/kind = %q/%q, want %q/%q from the registry defaults", tc.text, result.Surface, result.Kind, tc.surface, tc.kind)
		}
		if result.SpeakText != "done: "+tc.text || result.Action != "execute" || result.Locale != "en" {
			t.Fatalf("%q result = %#v, want speak text, execute action and locale defaults", tc.text, result)
		}
	}
}

func TestUnknownFallbackIntentWithoutFallbackIsAnError(t *testing.T) {
	cat := skills.New(skills.Options{})
	svc, err := assist.NewService(assist.Options{Matcher: cat.Matcher(), Executor: cat.Executor()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "copy last", Locale: "en"}); err == nil {
		t.Fatal("expected an error when a host utility matches and no fallback executor is configured")
	}
}

func TestDisabledIntentsFailClosed(t *testing.T) {
	cat := skills.New(skills.Options{DisabledIntents: []shortcuts.Intent{shortcuts.IntentWeather, shortcuts.IntentWikipedia}})
	generatorCalls := 0
	svc, err := assist.NewService(assist.Options{
		Matcher:   cat.Matcher(),
		Executor:  cat.Executor(),
		Generator: countingGenerator(&generatorCalls, "must not answer"),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for _, text := range []string{"weather in berlin", "tell me about linux"} {
		_, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: text, Locale: "en"})
		if !errors.Is(err, skills.ErrSkillDisabled) {
			t.Fatalf("Process(%q) error = %v, want %v", text, err, skills.ErrSkillDisabled)
		}
	}
	if generatorCalls != 0 {
		t.Fatalf("generator calls = %d, want a disabled skill to reach neither the network nor the model", generatorCalls)
	}
	// The other skills keep working.
	result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "what time is it", Locale: "en"})
	if err != nil || !strings.HasPrefix(result.Text, "It is") {
		t.Fatalf("time result = %#v err = %v", result, err)
	}
}

func TestDisabledIntentsCannotRemoveHomeAssistant(t *testing.T) {
	cat := skills.New(skills.Options{DisabledIntents: []shortcuts.Intent{shortcuts.IntentHomeAssistant}})
	svc, err := assist.NewService(assist.Options{Matcher: cat.Matcher(), Executor: cat.Executor()})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "turn on the kitchen light", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.ReasonCode != "not_configured" {
		t.Fatalf("result = %#v, want the fail-closed Home Assistant denial", result)
	}
}

// Multi-turn follow-ups, driven through the real Timer skill: a timer
// request without a duration asks "How long?" and the next utterance of the
// same session routes back to the Timer skill without matching a phrase.
func newMultiTurnService(t *testing.T, generator assist.Generator) (*assist.Service, *assist.InMemorySkillContextStore) {
	t.Helper()
	store := assist.NewInMemorySkillContextStore(0, nil)
	cat := skills.New(skills.Options{})
	svc, err := assist.NewService(assist.Options{
		Matcher:       cat.Matcher(),
		Executor:      cat.Executor(),
		Generator:     generator,
		SkillContexts: store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, store
}

func TestMultiTurnFollowupRoutesBackToSameIntent(t *testing.T) {
	svc, store := newMultiTurnService(t, nil)

	turn1, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "set a timer", Locale: "en", SessionKey: "user-1"})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if turn1.Text != "How long?" || turn1.ShortcutID != "timer" {
		t.Fatalf("turn 1 = %#v, want the timer follow-up question", turn1)
	}
	if store.Len() != 1 {
		t.Fatalf("store should hold 1 follow-up after turn 1; got %d", store.Len())
	}

	// "5 minutes" alone matches no codeword; the active context must route
	// it back to the timer skill.
	turn2, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "5 minutes", Locale: "en", SessionKey: "user-1"})
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if !strings.Contains(turn2.Text, "Timer set for 5 minutes") || turn2.ShortcutID != "timer" {
		t.Fatalf("turn 2 = %#v, want the timer confirmation", turn2)
	}
	if turn2.Surface != speechkit.AssistSurfaceActionAck || turn2.Kind != assist.KindUtilityAction {
		t.Fatalf("turn 2 surface/kind = %q/%q, want the utility defaults on a follow-up", turn2.Surface, turn2.Kind)
	}
	if store.Len() != 0 {
		t.Fatalf("store should clear after a completed follow-up; got %d", store.Len())
	}
}

func TestMultiTurnSecondUnparseableTurnFallsThroughToGenerator(t *testing.T) {
	generatorCalls := 0
	svc, store := newMultiTurnService(t, countingGenerator(&generatorCalls, "LLM took over"))

	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "set a timer", Locale: "en", SessionKey: "user-1"}); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	// The stored "awaiting_duration=1" marker reaches the skill through the
	// call context, so a second unparseable answer goes silent and the
	// service hands the turn to the generator instead of asking forever.
	turn2, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "banana", Locale: "en", SessionKey: "user-1"})
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if generatorCalls != 1 || turn2.Text != "LLM took over" {
		t.Fatalf("turn 2 = %#v (generator calls %d), want the generator fallthrough", turn2, generatorCalls)
	}
	if store.Len() != 0 {
		t.Fatalf("store should clear when the follow-up falls through; got %d", store.Len())
	}
}

func TestMultiTurnDifferentSessionsAreIsolated(t *testing.T) {
	svc, store := newMultiTurnService(t, nil)

	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "set a timer", Locale: "en", SessionKey: "alice"}); err != nil {
		t.Fatalf("alice turn 1: %v", err)
	}
	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "set a timer", Locale: "en", SessionKey: "bob"}); err != nil {
		t.Fatalf("bob turn 1: %v", err)
	}
	if store.Len() != 2 {
		t.Fatalf("expected 2 separate contexts (alice + bob); got %d", store.Len())
	}

	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "5 minutes", Locale: "en", SessionKey: "alice"}); err != nil {
		t.Fatalf("alice turn 2: %v", err)
	}
	if store.Len() != 1 {
		t.Fatalf("alice's completion should leave bob untouched; got %d", store.Len())
	}
	if _, ok := store.Get("bob"); !ok {
		t.Fatal("bob's context must survive alice's completion")
	}
}

func TestMultiTurnDisabledWhenSessionKeyEmpty(t *testing.T) {
	svc, store := newMultiTurnService(t, nil)
	if _, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: "set a timer", Locale: "en"}); err != nil {
		t.Fatalf("no-session turn: %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("an empty SessionKey must not write to the store")
	}
}

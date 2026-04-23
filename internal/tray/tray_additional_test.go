package tray

import "testing"

func TestStateConstantsStable(t *testing.T) {
	t.Parallel()
	// These values may appear in logs, metrics, or persisted state — keep
	// them stable across refactors.
	cases := map[State]string{
		StateIdle:       "idle",
		StateRecording:  "recording",
		StateProcessing: "processing",
		StateDone:       "done",
	}
	for state, want := range cases {
		if string(state) != want {
			t.Errorf("%s constant = %q, want %q", want, string(state), want)
		}
	}
}

func TestTooltipForKnownStates(t *testing.T) {
	t.Parallel()
	cases := map[State]string{
		StateIdle:       "SpeechKit",
		StateRecording:  "SpeechKit - Recording",
		StateProcessing: "SpeechKit - Processing",
		StateDone:       "SpeechKit - Done",
	}
	for state, want := range cases {
		if got := tooltipFor(state); got != want {
			t.Errorf("tooltipFor(%s) = %q, want %q", state, got, want)
		}
	}
}

func TestTooltipForUnknownStateFallsBack(t *testing.T) {
	t.Parallel()
	const defaultTooltip = "SpeechKit"
	if got := tooltipFor(State("bogus")); got != defaultTooltip {
		t.Errorf("tooltipFor(bogus) = %q, want %q", got, defaultTooltip)
	}
	if got := tooltipFor(State("")); got != defaultTooltip {
		t.Errorf("tooltipFor(empty) = %q, want %q", got, defaultTooltip)
	}
}

func TestTooltipDistinctPerState(t *testing.T) {
	t.Parallel()
	seen := map[string]State{}
	for _, state := range []State{StateIdle, StateRecording, StateProcessing, StateDone} {
		tooltip := tooltipFor(state)
		if prev, exists := seen[tooltip]; exists {
			t.Errorf("duplicate tooltip %q for states %s and %s", tooltip, prev, state)
		}
		seen[tooltip] = state
	}
}

func TestTooltipAlwaysStartsWithSpeechKit(t *testing.T) {
	t.Parallel()
	// Brand consistency guard: every tooltip begins with the product name.
	for _, state := range []State{StateIdle, StateRecording, StateProcessing, StateDone, State("bogus"), State("")} {
		got := tooltipFor(state)
		if len(got) < 9 || got[:9] != "SpeechKit" {
			t.Errorf("tooltipFor(%s) = %q, must start with \"SpeechKit\"", state, got)
		}
	}
}

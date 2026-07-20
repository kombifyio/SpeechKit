package live

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type sessionTestProvider struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	lastCfg   LiveConfig
	messages  chan *LiveMessage
	sentText  []string
}

type reconnectingSessionTestProvider struct {
	*sessionTestProvider
	reconnects int
}

func newSessionTestProvider() *sessionTestProvider {
	return &sessionTestProvider{
		messages: make(chan *LiveMessage, 8),
	}
}

func (p *sessionTestProvider) Connect(_ context.Context, cfg LiveConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = true
	p.lastCfg = cfg
	return nil
}

func (p *sessionTestProvider) SendAudio(_ []byte) error { return nil }

func (p *sessionTestProvider) SendAudioStreamEnd() error { return nil }

func (p *sessionTestProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	select {
	case msg := <-p.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *sessionTestProvider) SendText(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sentText = append(p.sentText, text)
	return nil
}
func (p *sessionTestProvider) SendToolResponse(_ ToolResponse) error { return nil }
func (p *sessionTestProvider) Name() string                          { return "session-test" }
func (p *sessionTestProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *reconnectingSessionTestProvider) Reconnect(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reconnects++
	return nil
}

func TestSessionTransitionsToProcessingAfterFinalInputTranscript(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 8)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{
		InputTranscript:     "hello there",
		InputTranscriptDone: true,
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-stateChanges:
			if state == StateProcessing {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s state", StateProcessing)
		}
	}
}

func TestSessionTransitionsToSpeakingWhenAudioArrives(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 8)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{
		Audio: []byte{1, 2, 3, 4},
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-stateChanges:
			if state == StateSpeaking {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s state", StateSpeaking)
		}
	}
}

func TestSessionReturnsToListeningWhenOutputTranscriptFinishesWithoutTurnComplete(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	outputDone := make(chan struct{}, 1)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
		OnOutputTranscript: func(_ string, done bool) {
			if done {
				outputDone <- struct{}{}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{
		OutputTranscript:     "finished answer",
		OutputTranscriptDone: true,
	}

	select {
	case <-outputDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output transcript callback")
	}
	if session.CurrentState() != StateListening {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateListening)
	}
}

// TestSessionKeepsSpeakingUntilTurnCompleteWhenTranscriptPrecedesAudio pins the
// Deepgram Voice Agent ordering: the full agent transcript (OutputTranscriptDone)
// arrives BEFORE the TTS audio has finished streaming. A premature return to
// Listening on transcript-done truncated the spoken answer (companion buffers
// audio until the turn ends, then plays it). The session must stay in the turn
// until an explicit Done, so all audio chunks are captured.
func TestSessionKeepsSpeakingUntilTurnCompleteWhenTranscriptPrecedesAudio(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) { stateChanges <- state },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "deepgram-agent-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	// Agent text lands first (Deepgram ConversationText), then audio streams.
	provider.messages <- &LiveMessage{
		OutputTranscript:     "die vollständige Antwort",
		OutputTranscriptDone: true,
	}
	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)

	// More audio chunks follow the transcript-done; the session must NOT have
	// returned to Listening in between (that would truncate playback).
	provider.messages <- &LiveMessage{Audio: []byte{5, 6, 7, 8}}
	assertNoStateWithin(t, stateChanges, StateListening, 150*time.Millisecond)
	if got := session.CurrentState(); got != StateSpeaking {
		t.Fatalf("state during audio = %s, want %s", got, StateSpeaking)
	}

	// Only the explicit turn-complete (AgentAudioDone -> Done) ends the turn.
	provider.messages <- &LiveMessage{Done: true}
	waitForState(t, stateChanges, StateListening)
}

func TestSessionDoesNotEmitDuplicateStateChangesForConsecutiveAudioChunks(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)

	provider.messages <- &LiveMessage{Audio: []byte{5, 6, 7, 8}}
	assertNoStateWithin(t, stateChanges, StateSpeaking, 50*time.Millisecond)

	if got := countBufferedState(stateChanges, StateSpeaking); got != 0 {
		t.Fatalf("duplicate %s state changes = %d, want 0", StateSpeaking, got)
	}
}

func TestSessionKeepsSpeakingStateForTranscriptPartialsDuringAudioTurn(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	outputTranscript := make(chan struct{}, 1)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
		OnOutputTranscript: func(_ string, _ bool) {
			outputTranscript <- struct{}{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{OutputTranscript: "partial answer"}

	select {
	case <-outputTranscript:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output transcript callback")
	}
	assertNoStateWithin(t, stateChanges, StateProcessing, 50*time.Millisecond)

	if session.CurrentState() != StateSpeaking {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateSpeaking)
	}
	if got := countBufferedState(stateChanges, StateProcessing); got != 0 {
		t.Fatalf("unexpected %s state changes during speaking turn = %d, want 0", StateProcessing, got)
	}
}

func TestSessionReturnsToListeningWhenAudioTurnDoesNotSendDone(t *testing.T) {
	provider := newSessionTestProvider()
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{Audio: []byte{1, 2, 3, 4}}
	waitForState(t, stateChanges, StateSpeaking)
	waitForState(t, stateChanges, StateListening)
}

func TestSessionShowsRecoveringStateDuringGoAwayReconnect(t *testing.T) {
	provider := &reconnectingSessionTestProvider{sessionTestProvider: newSessionTestProvider()}
	stateChanges := make(chan State, 16)
	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			stateChanges <- state
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()
	drainStateChanges(stateChanges)

	provider.messages <- &LiveMessage{GoAway: true}
	waitForState(t, stateChanges, StateRecovering)
	waitForState(t, stateChanges, StateListening)

	if provider.reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", provider.reconnects)
	}
}

func TestSessionHandlesMultiTurnTextDialog(t *testing.T) {
	provider := newSessionTestProvider()
	events := make(chan dialogueEvent, 16)
	session := NewSession(provider, Callbacks{
		OnInputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "user", text: text}
			}
		},
		OnOutputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "agent", text: text}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{Model: "gemini-live-test"}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	dialog := []struct {
		user  string
		agent string
	}{
		{user: "Ping one: collect three naming ideas.", agent: "Pong one: I have three concise options."},
		{user: "Ping two: make the strongest one warmer.", agent: "Pong two: the warm version is ready."},
		{user: "Ping three: compare it with the direct variant.", agent: "Pong three: the warmer option is friendlier."},
		{user: "Ping four: give me the final choice.", agent: "Pong four: choose the warm concise version."},
	}

	for _, turn := range dialog {
		if err := session.SendText(turn.user); err != nil {
			t.Fatalf("send text %q: %v", turn.user, err)
		}
		provider.messages <- &LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true}
		waitForDialogueEvent(t, events, "user", turn.user)

		provider.messages <- &LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true}
		waitForDialogueEvent(t, events, "agent", turn.agent)
	}

	sentText := providerSentText(provider)
	if len(sentText) != len(dialog) {
		t.Fatalf("sent text turns = %d, want %d: %#v", len(sentText), len(dialog), sentText)
	}
	for i, turn := range dialog {
		if sentText[i] != turn.user {
			t.Fatalf("sent text[%d] = %q, want %q", i, sentText[i], turn.user)
		}
	}
	if totalInteractions := len(dialog) * 2; totalInteractions <= 6 {
		t.Fatalf("dialog interactions = %d, want > 6", totalInteractions)
	}
	// The state machine transitions OutputTranscriptDone →
	// StateListening asynchronously. Without a poll the assertion races
	// the transition under -race (caught by CI in run 26278006120). Poll
	// up to 1 s so a slow state-machine tick doesn't flake the test;
	// production transitions complete in microseconds.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && session.CurrentState() != StateListening {
		time.Sleep(2 * time.Millisecond)
	}
	if session.CurrentState() != StateListening {
		t.Fatalf("current state = %s, want %s", session.CurrentState(), StateListening)
	}
}

func TestSessionAppliesInitialWorkflowStep(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Base role prompt.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog and clarify the goal.", MaxTurns: 1},
				{ID: "close", Instruction: "Close with the agreed next step.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	cfg := providerConfigSnapshot(provider)
	if !strings.Contains(cfg.FrameworkPrompt, "Base role prompt.") {
		t.Fatalf("FrameworkPrompt = %q, want base prompt", cfg.FrameworkPrompt)
	}
	if !strings.Contains(cfg.FrameworkPrompt, "[Current step: open]") {
		t.Fatalf("FrameworkPrompt = %q, want initial step marker", cfg.FrameworkPrompt)
	}
	if !strings.Contains(cfg.FrameworkPrompt, "Open the dialog") {
		t.Fatalf("FrameworkPrompt = %q, want initial step instruction", cfg.FrameworkPrompt)
	}
}

func TestSessionAutoAdvancesWorkflowStepAfterMaxTurns(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate the session.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog.", MaxTurns: 1},
				{ID: "decide", Instruction: "Drive toward a concrete decision.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{InputTranscript: "We should start.", InputTranscriptDone: true}

	update := waitForSentText(t, provider)
	if !strings.Contains(update, "Host instruction update") {
		t.Fatalf("instruction update = %q, want host update marker", update)
	}
	if !strings.Contains(update, "Step: decide") {
		t.Fatalf("instruction update = %q, want next step", update)
	}
	if !strings.Contains(update, "Drive toward a concrete decision.") {
		t.Fatalf("instruction update = %q, want next step instruction", update)
	}
}

func TestSessionAdvanceWorkflowStepSendsInstructionUpdate(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate the session.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog.", MaxTurns: 3},
				{ID: "wrap", Instruction: "Wrap up with the action items.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	if err := session.AdvanceWorkflowStep(ctx, "test"); err != nil {
		t.Fatalf("AdvanceWorkflowStep() error = %v", err)
	}

	update := waitForSentText(t, provider)
	if !strings.Contains(update, "Step: wrap") {
		t.Fatalf("instruction update = %q, want wrap step", update)
	}
	if !strings.Contains(update, "Wrap up with the action items.") {
		t.Fatalf("instruction update = %q, want wrap instruction", update)
	}
}

func TestSessionEvaluatesWorkflowDialogSimulation(t *testing.T) {
	provider := newSessionTestProvider()
	events := make(chan dialogueEvent, 16)
	session := NewSession(provider, Callbacks{
		OnInputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "user", text: text}
			}
		},
		OnOutputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "agent", text: text}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate an interactive product strategy meeting.",
		Workflow: &WorkflowConfig{
			SequenceID: "strategy-meeting",
			Steps: []WorkflowStep{
				{ID: "frame", Instruction: "Clarify the goal and constraints.", MaxTurns: 3},
				{ID: "diverge", Instruction: "Explore alternatives and blind spots.", MaxTurns: 3},
				{ID: "converge", Instruction: "Converge on the next step.", MaxTurns: 3},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	dialog := []struct {
		stepID string
		user   string
		agent  string
		rule   dialogueRule
	}{
		{
			stepID: "frame",
			user:   "We need to plan the launch meeting.",
			agent:  "Goal first: align the launch decision, capture constraints, and define a useful outcome.",
			rule:   dialogueRule{Contains: []string{"goal", "constraints", "outcome"}, NotContains: []string{"final recommendation"}},
		},
		{
			stepID: "diverge",
			user:   "The main constraint is only two engineers.",
			agent:  "Alternative paths: reduce scope or stagger rollout. One blind spot is support load after release.",
			rule:   dialogueRule{Contains: []string{"alternative", "blind spot"}, NotContains: []string{"diagnosis"}},
		},
		{
			stepID: "converge",
			user:   "Let us pick the staged rollout.",
			agent:  "Recommendation: choose staged rollout and set the next step as a 30-minute owner review.",
			rule:   dialogueRule{Contains: []string{"recommendation", "next step"}, NotContains: []string{"another hour"}},
		},
	}

	var transcript []dialogueTurn
	for i, turn := range dialog {
		if err := session.SendText(turn.user); err != nil {
			t.Fatalf("send text %q: %v", turn.user, err)
		}
		provider.messages <- &LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true}
		waitForDialogueEvent(t, events, "user", turn.user)

		provider.messages <- &LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true}
		waitForDialogueEvent(t, events, "agent", turn.agent)
		transcript = append(transcript, dialogueTurn{
			Speaker: "agent",
			StepID:  turn.stepID,
			Text:    turn.agent,
			Rule:    turn.rule,
		})

		if i < len(dialog)-1 {
			if err := session.AdvanceWorkflowStep(ctx, "simulation"); err != nil {
				t.Fatalf("advance workflow after %s: %v", turn.stepID, err)
			}
		}
	}

	if err := evaluateDialogueTranscript(transcript); err != nil {
		t.Fatalf("dialog simulation failed instruction checks: %v", err)
	}
}

type dialogueRule struct {
	Contains    []string
	NotContains []string
}

type dialogueTurn struct {
	Speaker string
	StepID  string
	Text    string
	Rule    dialogueRule
}

func evaluateDialogueTranscript(turns []dialogueTurn) error {
	for i, turn := range turns {
		text := strings.ToLower(turn.Text)
		for _, want := range turn.Rule.Contains {
			if !strings.Contains(text, strings.ToLower(want)) {
				return fmt.Errorf("turn %d (%s/%s) missing %q in %q", i+1, turn.Speaker, turn.StepID, want, turn.Text)
			}
		}
		for _, forbidden := range turn.Rule.NotContains {
			if strings.Contains(text, strings.ToLower(forbidden)) {
				return fmt.Errorf("turn %d (%s/%s) unexpectedly contains %q in %q", i+1, turn.Speaker, turn.StepID, forbidden, turn.Text)
			}
		}
	}
	return nil
}

type dialogueEvent struct {
	side string
	text string
}

func waitForDialogueEvent(t *testing.T, ch <-chan dialogueEvent, side, text string) {
	t.Helper()
	select {
	case event := <-ch:
		if event.side != side || event.text != text {
			t.Fatalf("dialogue event = %#v, want side=%q text=%q", event, side, text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s dialogue event %q", side, text)
	}
}

func providerSentText(provider *sessionTestProvider) []string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]string(nil), provider.sentText...)
}

func providerConfigSnapshot(provider *sessionTestProvider) LiveConfig {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.lastCfg
}

func waitForSentText(t *testing.T, provider *sessionTestProvider) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		sent := providerSentText(provider)
		if len(sent) > 0 {
			return sent[len(sent)-1]
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for provider text")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func drainStateChanges(ch <-chan State) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForState(t *testing.T, ch <-chan State, want State) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case state := <-ch:
			if state == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func countBufferedState(ch <-chan State, want State) int {
	count := 0
	for {
		select {
		case state := <-ch:
			if state == want {
				count++
			}
		default:
			return count
		}
	}
}

func assertNoStateWithin(t *testing.T, ch <-chan State, forbidden State, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case state := <-ch:
			if state == forbidden {
				t.Fatalf("unexpected %s state change within %s", forbidden, timeout)
			}
		case <-timer.C:
			return
		}
	}
}

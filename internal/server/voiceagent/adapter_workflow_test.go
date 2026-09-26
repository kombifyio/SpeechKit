//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/voiceeval"
)

func TestAdapter_StartFrameEmitsInitialSequenceStep(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{frame: LiveConfigFrame{
		SequenceID: "meeting",
		StepID:     "opening",
		StepIndex:  0,
		StepCount:  2,
	}}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "meeting"})

	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	if stateMsg.Type != MsgState || stateMsg.State != "listening" {
		t.Fatalf("expected state listening, got %+v", stateMsg)
	}

	var stepMsg SequenceStepFrame
	readJSONFrame(t, env.conn, &stepMsg)
	if stepMsg.Type != MsgSequenceStep || stepMsg.SequenceID != "meeting" ||
		stepMsg.StepID != "opening" || stepMsg.StepIndex != 0 || stepMsg.Status != "entered" {
		t.Fatalf("unexpected sequence step frame: %+v", stepMsg)
	}
}

func TestAdapter_AdvanceStepUpdatesProviderAndEmitsSequenceFrames(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{
		frame: LiveConfigFrame{
			SequenceID: "meeting",
			StepID:     "opening",
			StepIndex:  0,
			StepCount:  2,
		},
		stepFrames: map[int]LiveConfigFrame{
			1: {
				SequenceID:      "meeting",
				StepID:          "discussion",
				StepIndex:       1,
				StepCount:       2,
				StepInstruction: "Moderate the main discussion.",
				SystemPrompt:    "Role prompt\n\n[Current step: discussion]\nModerate the main discussion.",
			},
		},
	}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "meeting"})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	var enteredOpening SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredOpening)

	advanceFrame, _ := json.Marshal(AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "host"})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, advanceFrame); err != nil {
		t.Fatalf("write advance_step: %v", err)
	}

	var completed SequenceStepFrame
	readJSONFrame(t, env.conn, &completed)
	if completed.Status != "completed" || completed.StepID != "opening" {
		t.Fatalf("completed frame = %+v", completed)
	}
	var enteredDiscussion SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredDiscussion)
	if enteredDiscussion.Status != "entered" || enteredDiscussion.StepID != "discussion" || enteredDiscussion.StepIndex != 1 {
		t.Fatalf("entered frame = %+v", enteredDiscussion)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		updates := append([]LiveConfigFrame(nil), provider.updatedConfigs...)
		provider.mu.Unlock()
		if len(updates) == 1 && updates[0].StepID == "discussion" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive instruction update")
}

func TestAdapter_EvaluatesWorkflowDialogSimulation(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{
		frame: LiveConfigFrame{
			SequenceID:      "strategy-meeting",
			StepID:          "frame",
			StepIndex:       0,
			StepCount:       3,
			StepInstruction: "Clarify the goal and constraints.",
			StepMaxTurns:    3,
		},
		stepFrames: map[int]LiveConfigFrame{
			1: {
				SequenceID:      "strategy-meeting",
				StepID:          "diverge",
				StepIndex:       1,
				StepCount:       3,
				StepInstruction: "Explore alternatives and blind spots.",
				StepMaxTurns:    3,
				SystemPrompt:    "Explore alternatives and blind spots.",
			},
			2: {
				SequenceID:      "strategy-meeting",
				StepID:          "converge",
				StepIndex:       2,
				StepCount:       3,
				StepInstruction: "Converge on a concrete next step.",
				StepMaxTurns:    3,
				SystemPrompt:    "Converge on a concrete next step.",
			},
		},
	}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "strategy-meeting"})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	var enteredFrame SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredFrame)

	dialog := []struct {
		stepID string
		user   string
		agent  string
		rule   voiceeval.Rule
	}{
		{
			stepID: "frame",
			user:   "We need to plan the launch meeting.",
			agent:  "Goal first: define the launch decision, constraints, and useful outcome.",
			rule:   voiceeval.Rule{Contains: []string{"goal", "constraints", "outcome"}, NotContains: []string{"final recommendation"}},
		},
		{
			stepID: "diverge",
			user:   "We have two engineers.",
			agent:  "Alternative paths: reduce scope or stage rollout. A blind spot is support load.",
			rule:   voiceeval.Rule{Contains: []string{"alternative", "blind spot"}, NotContains: []string{"diagnosis"}},
		},
		{
			stepID: "converge",
			user:   "Staged rollout sounds right.",
			agent:  "Recommendation: choose staged rollout and make the next step an owner review.",
			rule:   voiceeval.Rule{Contains: []string{"recommendation", "next step"}, NotContains: []string{"another hour"}},
		},
	}

	var transcript []voiceeval.Turn
	for i, turn := range dialog {
		provider.push(&LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true})
		var input TranscriptFrame
		readJSONFrame(t, env.conn, &input)
		if input.Type != MsgInputTranscript || input.Text != turn.user || !input.Done {
			t.Fatalf("input transcript frame = %+v", input)
		}

		provider.push(&LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true})
		var output TranscriptFrame
		readJSONFrame(t, env.conn, &output)
		if output.Type != MsgOutputTranscript || output.Text != turn.agent || !output.Done {
			t.Fatalf("output transcript frame = %+v", output)
		}
		transcript = append(transcript, voiceeval.Turn{
			Speaker: "agent",
			StepID:  turn.stepID,
			Text:    turn.agent,
			Rule:    turn.rule,
		})

		if i < len(dialog)-1 {
			advanceFrame, _ := json.Marshal(AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "simulation"})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if err := env.conn.Write(ctx, websocket.MessageText, advanceFrame); err != nil {
				cancel()
				t.Fatalf("write advance_step: %v", err)
			}
			cancel()
			var completed SequenceStepFrame
			readJSONFrame(t, env.conn, &completed)
			if completed.Status != "completed" || completed.StepID != turn.stepID {
				t.Fatalf("completed frame = %+v, want step %q", completed, turn.stepID)
			}
			var entered SequenceStepFrame
			readJSONFrame(t, env.conn, &entered)
			if entered.Status != "entered" || entered.StepID != dialog[i+1].stepID {
				t.Fatalf("entered frame = %+v, want step %q", entered, dialog[i+1].stepID)
			}
		}
	}

	if err := voiceeval.EvaluateTranscript(transcript); err != nil {
		t.Fatalf("dialog simulation failed instruction checks: %v", err)
	}
}

func TestAdapter_ProviderToolCallRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "summarize",
			Args: map[string]any{"text": "raw notes"},
		}},
		ProviderMetadata: map[string]any{"provider_event": "fake.tool.call"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgToolCall {
		t.Fatalf("expected tool_call, got %s body=%s", typeName, string(raw))
	}
	var frame ToolCallFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal tool_call: %v", err)
	}
	if frame.ID != "call-1" || frame.Name != "summarize" || frame.Args["text"] != "raw notes" {
		t.Fatalf("unexpected tool_call frame: %+v", frame)
	}
	if frame.EventType != EventToolCall || !eventTypesContain(frame.EventTypes, EventToolCall) {
		t.Fatalf("tool_call event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.tool.call" {
		t.Fatalf("tool_call provider metadata = %#v", frame.ProviderMetadata)
	}
}

func TestAdapter_ToolResponseForwardedToProvider(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	response, _ := json.Marshal(ToolResponseFrame{
		Type: MsgToolResponse,
		ID:   "call-1",
		Name: "summarize",
		Response: map[string]any{
			"summary": "done",
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, response); err != nil {
		t.Fatalf("write tool_response: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		got := append([]ToolResponseFrame(nil), provider.toolResponses...)
		provider.mu.Unlock()
		if len(got) == 1 && got[0].ID == "call-1" && got[0].Response["summary"] == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive tool response")
}

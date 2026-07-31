import { describe, expect, it } from "vitest";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../src/core/turns.js";
import type { SpeechKitVoiceEvent } from "../src/core/voice-surface.js";

function ev(partial: Partial<SpeechKitVoiceEvent> & { type: SpeechKitVoiceEvent["type"] }): SpeechKitVoiceEvent {
  return { surface: "floating_panel", mode: "voice_agent", ...partial };
}

describe("reduceVoiceAgentTurns", () => {
  it("cumulative drafts replace the open user turn", () => {
    let turns: VoiceAgentTurn[] = [];
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.transcript_draft", text: "he" }));
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.transcript_draft", text: "hello" }));
    expect(turns).toEqual([{ role: "user", text: "hello", final: false, interrupted: false }]);
  });

  it("final closes the open turn and keeps draft text when empty", () => {
    let turns: VoiceAgentTurn[] = [];
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.transcript_draft", text: "keep" }));
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.transcript_final", final: true }));
    expect(turns).toEqual([{ role: "user", text: "keep", final: true, interrupted: false }]);
  });

  it("non-empty final without an open turn appends a closed turn", () => {
    const turns = reduceVoiceAgentTurns([], ev({ type: "voice.transcript_final", text: "hi", final: true }));
    expect(turns).toEqual([{ role: "user", text: "hi", final: true, interrupted: false }]);
  });

  it("barge-in closes and marks the open agent turn; next agent event starts fresh", () => {
    let turns: VoiceAgentTurn[] = [];
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.agent_turn", output_text: "long expl", final: false }));
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.barge_in" }));
    expect(turns).toEqual([{ role: "agent", text: "long expl", final: true, interrupted: true }]);
    turns = reduceVoiceAgentTurns(turns, ev({ type: "voice.agent_turn", output_text: "short", final: false }));
    expect(turns).toHaveLength(2);
    expect(turns[1]).toEqual({ role: "agent", text: "short", final: false, interrupted: false });
  });

  it("unrelated events return the same array reference", () => {
    const turns = reduceVoiceAgentTurns([], ev({ type: "voice.transcript_draft", text: "x" }));
    const next = reduceVoiceAgentTurns(turns, ev({ type: "voice.tts_started" }));
    expect(next).toBe(turns);
  });

  it("empty draft opens no turn", () => {
    const turns = reduceVoiceAgentTurns([], ev({ type: "voice.transcript_draft", text: "" }));
    expect(turns).toEqual([]);
  });
});

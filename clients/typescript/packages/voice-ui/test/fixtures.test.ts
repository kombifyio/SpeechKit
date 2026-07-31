import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../src/core/turns.js";
import {
  SPEECHKIT_VOICE_SURFACE_VERSION,
  SPEECHKIT_VOICE_UI_VERSION,
  createSpeechKitVoiceSessionState,
  isSpeechKitVoiceEventType,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceEvent,
  type SpeechKitVoiceSessionStatus
} from "../src/core/voice-surface.js";

const fixturesDir = join(dirname(fileURLToPath(import.meta.url)), "..", "spec", "fixtures");

interface TurnFixtureCase {
  name: string;
  events: SpeechKitVoiceEvent[];
  expected_turns: VoiceAgentTurn[];
  expected_status: SpeechKitVoiceSessionStatus;
}

const turnFixture = JSON.parse(
  readFileSync(join(fixturesDir, "voice-ui-turns.v1.json"), "utf8")
) as { version: string; cases: TurnFixtureCase[] };

describe("voice-ui-turns.v1 fixture replay", () => {
  it("carries the kit contract version", () => {
    expect(turnFixture.version).toBe(SPEECHKIT_VOICE_UI_VERSION);
    expect(turnFixture.cases.length).toBeGreaterThan(0);
  });

  for (const testCase of turnFixture.cases) {
    it(testCase.name, () => {
      let turns: VoiceAgentTurn[] = [];
      let state = createSpeechKitVoiceSessionState({
        surface: testCase.events[0]?.surface ?? "floating_panel",
        mode: testCase.events[0]?.mode ?? "voice_agent",
        session: {}
      });
      for (const event of testCase.events) {
        expect(isSpeechKitVoiceEventType(event.type)).toBe(true);
        turns = reduceVoiceAgentTurns(turns, event);
        state = reduceSpeechKitVoiceEvent(state, event);
      }
      expect(turns).toEqual(testCase.expected_turns);
      expect(state.status).toBe(testCase.expected_status);
    });
  }
});

describe("vendored speechkit.voice_surface.v1 fixture", () => {
  it("parses and carries the canonical version and event vocabulary", () => {
    const fixture = JSON.parse(
      readFileSync(join(fixturesDir, "speechkit-voice-surface.v1.json"), "utf8")
    ) as { version: string; events: Array<{ type: string }> };
    expect(fixture.version).toBe(SPEECHKIT_VOICE_SURFACE_VERSION);
    for (const event of fixture.events) {
      expect(isSpeechKitVoiceEventType(event.type)).toBe(true);
    }
  });
});

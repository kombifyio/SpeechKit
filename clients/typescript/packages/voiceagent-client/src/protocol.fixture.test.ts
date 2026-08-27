import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import type {
  AdvanceStepFrame,
  ClientFrame,
  ErrorFrame,
  EventFrame,
  SequenceStepFrame,
  ServerFrame,
  SessionEndFrame,
  StartFrame,
  StateFrame,
  TextFrame,
  ToolCallFrame,
  ToolResponseFrame,
  TranscriptFrame,
} from "./protocol.js";

/**
 * Consumer drift-check: replays the golden frames from
 * docs/server/fixtures/voiceagent.v1.json (the interchange artifact produced
 * by internal/server/voiceagent/protocol.go and pinned by its
 * protocol_fixture_test.go) against the protocol.ts frame types.
 */

const fixtureUrl = new URL(
  "../../../../../docs/server/fixtures/voiceagent.v1.json",
  import.meta.url,
);

interface Fixture {
  contract: string;
  frames: Record<string, { type: string } & Record<string, unknown>>;
}

const fixture: Fixture = JSON.parse(readFileSync(fileURLToPath(fixtureUrl), "utf8"));

function frame<T>(name: string): T {
  const entry = fixture.frames[name];
  expect(entry, `fixture frame ${name}`).toBeDefined();
  return entry as T;
}

/** Frame types protocol.ts declares on the ClientFrame union. */
const CLIENT_TYPES = ["start", "text", "tool_response", "advance_step", "audio_end", "ping", "stop"] as const;

/**
 * Client frame types the Go producer defines that protocol.ts does not yet
 * model (tracked drift, see the protocol.go SSOT header): `cancel` is the
 * tap-to-interrupt control frame.
 */
const CLIENT_TYPES_PENDING_TS = ["cancel"] as const;

/** Frame types protocol.ts declares on the ServerFrame union. */
const SERVER_TYPES = [
  "state",
  "input_transcript",
  "output_transcript",
  "tool_call",
  "sequence_step",
  "event",
  "interrupted",
  "error",
  "session_end",
  "pong",
] as const;

describe("voiceagent.v1 fixture", () => {
  it("declares the v1 contract id", () => {
    expect(fixture.contract).toBe("speechkit.voiceagent.v1");
  });

  it("covers every frame type protocol.ts declares (and nothing unknown)", () => {
    const known = new Set<string>([...CLIENT_TYPES, ...CLIENT_TYPES_PENDING_TS, ...SERVER_TYPES]);
    const seen = new Set(Object.values(fixture.frames).map((f) => f.type));
    for (const entry of Object.values(fixture.frames)) {
      expect(known, `unknown frame type ${entry.type}`).toContain(entry.type);
    }
    for (const type of known) {
      expect(seen, `no golden frame for type ${type}`).toContain(type);
    }
  });

  it("start frames decode as StartFrame", () => {
    const start = frame<StartFrame>("start");
    expect(start.type).toBe("start");
    expect(start.persona_id).toBe("brainstorm");
    expect(start.provider).toBe("deepgram");
    expect(start.media_transport).toBe("websocket");
    expect(start.thinking).toBe("low");

    const full = frame<StartFrame>("start_full");
    expect(full.media_transport).toBe("livekit");
    expect(full.activity_detection?.automatic).toBe(true);
    expect(full.activity_detection?.silence_duration_ms).toBe(600);
    expect(full.speaker?.providerProfileId).toBe("speaker.deepgram.nova-3");
    expect(full.system_prompt_override).toBeTruthy();
  });

  it("client frames decode into the ClientFrame union", () => {
    const text = frame<TextFrame>("text");
    expect(text.text).toBe("Wie ist das Wetter in Berlin?");

    const toolResponse = frame<ToolResponseFrame>("tool_response");
    expect(toolResponse.id).toBe("t1");
    expect(toolResponse.name).toBe("weather");
    expect(toolResponse.response).toEqual({ city: "Berlin", temperature_c: 21.5 });

    const advance = frame<AdvanceStepFrame>("advance_step");
    expect(advance.step_id).toBe("step-2");

    for (const name of ["audio_end", "ping", "stop"] as const) {
      const control = frame<ClientFrame>(name);
      expect(control.type).toBe(name);
      expect(Object.keys(control)).toEqual(["type"]);
    }
  });

  it("state frames decode as StateFrame", () => {
    const ready = frame<StateFrame>("state_session_ready");
    expect(ready.state).toBe("listening");
    expect(ready.event_type).toBe("session_ready");

    const speaking = frame<StateFrame>("state");
    expect(speaking.state).toBe("speaking");
  });

  it("transcript frames decode for both directions", () => {
    const partial = frame<TranscriptFrame>("input_transcript_partial");
    expect(partial.type).toBe("input_transcript");
    expect(partial.done).toBe(false);
    expect(partial.text).toBe("wie ist das");

    const final = frame<TranscriptFrame>("input_transcript_final");
    expect(final.done).toBe(true);

    // Speaker-attribution fields are additive server-side extras protocol.ts
    // does not yet surface; the frame must still narrow as TranscriptFrame.
    const attributed = frame<TranscriptFrame>("input_transcript_speaker");
    expect(attributed.done).toBe(true);

    const output = frame<TranscriptFrame>("output_transcript");
    expect(output.type).toBe("output_transcript");
    expect(output.text).toBe("In Berlin sind es 21 Grad und sonnig.");
  });

  it("tool_call, sequence_step, event, interrupted decode", () => {
    const toolCall = frame<ToolCallFrame>("tool_call");
    expect(toolCall.id).toBe("t1");
    expect(toolCall.args).toEqual({ city: "Berlin" });

    const step = frame<SequenceStepFrame>("sequence_step");
    expect(step.step_id).toBe("step-2");
    expect(step.step_index).toBe(2);
    expect(step.status).toBe("entered");

    const event = frame<EventFrame>("event");
    expect(event.event_type).toBe("turn_end");

    const interrupted = frame<ServerFrame>("interrupted");
    expect(interrupted.type).toBe("interrupted");
  });

  it("error, session_end, pong decode", () => {
    const error = frame<ErrorFrame>("error");
    expect(error.code).toBe("provider_unavailable");
    expect(error.message).toBeTruthy();

    const end = frame<SessionEndFrame>("session_end");
    expect(end.reason).toBe("idle");

    const pong = frame<ServerFrame>("pong");
    expect(pong.type).toBe("pong");
  });
});

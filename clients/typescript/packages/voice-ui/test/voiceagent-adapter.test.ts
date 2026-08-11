import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SessionHooks } from "@kombifyio/speechkit-voiceagent-client/browser";

const openBrowserSession = vi.fn();
vi.mock("@kombifyio/speechkit-voiceagent-client/browser", () => ({
  openBrowserSession: (options: unknown) => openBrowserSession(options)
}));

import { createVoiceAgentUiController } from "../src/adapters/voiceagent.js";
import type { SpeechKitVoiceSessionState } from "../src/core/voice-surface.js";

interface CapturedOpen {
  hooks: SessionHooks;
  onPlaybackLevel?: (level: number) => void;
  start: Record<string, unknown>;
  flushPlayback: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
  attachMicrophone: ReturnType<typeof vi.fn>;
}

function fakeStream(): MediaStream {
  return { getTracks: () => [] } as unknown as MediaStream;
}

function arm(): { captured: () => CapturedOpen } {
  let captured: CapturedOpen | null = null;
  openBrowserSession.mockImplementation((options) => {
    const flushPlayback = vi.fn();
    const close = vi.fn();
    const attachMicrophone = vi.fn(() => () => {});
    captured = {
      hooks: options.hooks,
      onPlaybackLevel: options.onPlaybackLevel,
      start: options.start,
      flushPlayback,
      close,
      attachMicrophone
    };
    return Promise.resolve({
      session: {},
      socket: {},
      ticket: { session_id: "s-1", ticket: "t" },
      attachMicrophone,
      playChunk: vi.fn(),
      flushPlayback,
      close
    });
  });
  return {
    captured: () => {
      if (!captured) throw new Error("openBrowserSession not called");
      return captured;
    }
  };
}

function makeController() {
  return createVoiceAgentUiController({
    serverUrl: "http://localhost:8080",
    token: "tok",
    start: { provider: "deepgram", persona_id: "default" },
    getAudioStream: () => Promise.resolve(fakeStream())
  });
}

beforeEach(() => {
  openBrowserSession.mockReset();
});

describe("createVoiceAgentUiController", () => {
  it("reports the voiceagent contract with provider context", () => {
    const controller = makeController();
    expect(controller.contract.version).toBe("speechkit.voice_surface.v1");
    expect(controller.contract.mode).toBe("voice_agent");
    expect(controller.contract.transport).toBe("voiceagent_ws_ticket");
    expect(controller.contract.provider).toEqual({ provider_id: "deepgram" });
  });

  it("passes the start frame through and enters capturing after start", async () => {
    const { captured } = arm();
    const controller = makeController();
    await controller.start("voice_agent");
    expect(captured().start).toEqual({ provider: "deepgram", persona_id: "default" });
    expect(captured().attachMicrophone).toHaveBeenCalledTimes(1);
    expect(controller.getState().status).toBe("capturing");
  });

  it("reduces transcripts and agent turns through the canonical reducer", async () => {
    const { captured } = arm();
    const controller = makeController();
    await controller.start();
    const hooks = captured().hooks;
    hooks.onUserTranscript?.("what is", false);
    hooks.onUserTranscript?.("what is the kit", false);
    expect(controller.getState().transcript_draft).toBe("what is the kit");
    hooks.onUserTranscript?.("", true);
    expect(controller.getState().transcript_final).toBe("what is the kit");
    expect(controller.getState().status).toBe("processing");
    hooks.onAgentTranscript?.("It is ", false);
    hooks.onAgentTranscript?.("the kit.", true);
    expect(controller.getState().spoken_response).toBe("It is the kit.");
    expect(controller.getState().status).toBe("speaking");
  });

  it("flushes playback and emits barge_in on provider interruption", async () => {
    const { captured } = arm();
    const controller = makeController();
    await controller.start();
    captured().hooks.onInterrupted?.({ type: "interrupted" });
    expect(captured().flushPlayback).toHaveBeenCalled();
    expect(controller.getState().barge_in).toBe(true);
    expect(controller.getState().status).toBe("capturing");
  });

  it("forwards playback levels to level subscribers", async () => {
    const { captured } = arm();
    const controller = makeController();
    const levels: Array<[number, string]> = [];
    controller.subscribeLevel((level, source) => levels.push([level, source]));
    await controller.start();
    captured().onPlaybackLevel?.(0.5);
    expect(levels).toContainEqual([0.5, "output"]);
  });

  it("settles to idle when the session closes remotely", async () => {
    const { captured } = arm();
    const controller = makeController();
    await controller.start();
    captured().hooks.onClose?.("go_away");
    expect(captured().close).toHaveBeenCalled();
    expect(controller.getState().status).toBe("idle");
  });

  it("emits a denial envelope when the connection fails", async () => {
    openBrowserSession.mockRejectedValue(new Error("boom"));
    const controller = makeController();
    const states: SpeechKitVoiceSessionState[] = [];
    controller.subscribe((state) => states.push(state));
    await controller.start();
    const last = states[states.length - 1];
    expect(last?.status).toBe("denied");
    expect(last?.denial?.error_code).toBe("session_connect_failed");
  });

  it("emits a denial envelope when the microphone is refused", async () => {
    const controller = createVoiceAgentUiController({
      serverUrl: "http://localhost:8080",
      getAudioStream: () => Promise.reject(new Error("denied by user"))
    });
    await controller.start();
    expect(controller.getState().status).toBe("denied");
    expect(controller.getState().denial?.error_code).toBe("mic_permission_denied");
    expect(openBrowserSession).not.toHaveBeenCalled();
  });
});

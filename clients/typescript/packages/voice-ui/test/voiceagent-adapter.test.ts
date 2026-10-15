import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SessionHooks } from "@kombifyio/speechkit-voiceagent-client/browser";
import { VoiceAgentSession } from "@kombifyio/speechkit-voiceagent-client";

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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function trackedStream() {
  const track = { readyState: "live", stop() { this.readyState = "ended"; } };
  return { stream: { getTracks: () => [track] } as unknown as MediaStream, track };
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
  it("keeps repeated Start actions on the same pending microphone acquisition", async () => {
    const { captured } = arm();
    const mic = deferred<MediaStream>();
    const { stream, track } = trackedStream();
    let requesting = false;
    const controller = createVoiceAgentUiController({
      serverUrl: "http://localhost:8080",
      getAudioStream: () => {
        if (requesting) return Promise.reject(new DOMException("microphone busy", "NotReadableError"));
        requesting = true;
        return mic.promise;
      }
    });
    const starting = controller.start();
    await controller.start();
    mic.resolve(stream);
    await starting;
    expect(controller.getState().status).toBe("capturing");
    expect(track.readyState).toBe("live");
    controller.dispose();
    expect(track.readyState).toBe("ended");
    expect(captured().close).toHaveBeenCalled();
  });

  it.each(["stop", "cancel", "dispose"] as const)(
    "%s fences a pending microphone grant without minting or reviving capture",
    async (action) => {
      const mic = deferred<MediaStream>();
      const { stream, track } = trackedStream();
      const controller = createVoiceAgentUiController({
        serverUrl: "http://localhost:8080",
        getAudioStream: () => mic.promise
      });
      const starting = controller.start();
      expect(controller.getState().status).toBe("processing");
      controller[action]();
      mic.resolve(stream);
      await starting;
      expect(track.readyState).toBe("ended");
      expect(openBrowserSession).not.toHaveBeenCalled();
      expect(controller.getState().status).not.toBe("capturing");
    }
  );

  it.each(["stop", "cancel", "dispose"] as const)(
    "%s aborts pending setup and isolates its late result from a replacement session",
    async (action) => {
      const pending = deferred<Awaited<ReturnType<typeof openBrowserSession>>>();
      const mintStarted = deferred<void>();
      const { stream, track } = trackedStream();
      let signal: AbortSignal | undefined;
      let oldHooks: SessionHooks = {};
      let attached = false;
      let closed = false;
      openBrowserSession.mockImplementationOnce((options) => {
        signal = options.signal;
        oldHooks = options.hooks;
        mintStarted.resolve();
        return pending.promise;
      });
      const controller = createVoiceAgentUiController({
        serverUrl: "http://localhost:8080",
        getAudioStream: () => Promise.resolve(stream)
      });
      const starting = controller.start();
      await mintStarted.promise;
      controller[action]();
      expect(signal?.aborted).toBe(true);
      expect(track.readyState).toBe("ended");
      if (action !== "dispose") {
        arm();
        await controller.start();
      }
      pending.resolve({
        session: {}, socket: {}, ticket: { session_id: "obsolete", ticket: "t" },
        attachMicrophone() { attached = true; return () => {}; },
        close() { closed = true; }, flushPlayback() {}, playChunk() {}
      });
      await starting;
      oldHooks.onClose?.("closed");
      oldHooks.onError?.(new Error("late failure"));
      oldHooks.onUserTranscript?.("obsolete transcript", false);
      expect(attached).toBe(false);
      expect(closed).toBe(true);
      if (action !== "dispose") {
        expect(controller.getState().status).toBe("capturing");
        expect(controller.getState().transcript_draft).toBe("");
      }
      controller.dispose();
    }
  );

  it("cancels remote generation and suppresses late audio until interruption is acknowledged", async () => {
    const wire = new EventTarget();
    const controls: unknown[] = [];
    let hooks: SessionHooks = {};
    let output = "";
    const socket = {
      send(data: unknown) { if (typeof data === "string") controls.push(JSON.parse(data)); },
      close() {},
      addEventListener: wire.addEventListener.bind(wire),
      removeEventListener: wire.removeEventListener.bind(wire)
    };
    const voice = new VoiceAgentSession(socket, {
      start: {},
      hooks: {
        onInterrupted: (frame) => hooks.onInterrupted?.(frame),
        onAudio: (audio) => hooks.onAudio?.(audio)
      }
    });
    wire.dispatchEvent(new Event("open"));
    wire.dispatchEvent(new MessageEvent("message", { data: JSON.stringify({
      type: "state", state: "listening", event_type: "session_ready"
    }) }));
    await voice.ready;
    openBrowserSession.mockImplementationOnce((options) => {
      hooks = options.hooks;
      return Promise.resolve({
        session: voice, socket, ticket: { session_id: "s", ticket: "t" },
        attachMicrophone: () => () => {},
        playChunk() { output += "audio"; },
        flushPlayback() { output = ""; },
        close() { voice.close(); }
      });
    });
    const controller = makeController();
    await controller.start();
    const interruptions = () => controller.getState().events
      .filter(event => event.type === "voice.barge_in")
      .map(event => event.reason_code);
    wire.dispatchEvent(new MessageEvent("message", { data: new ArrayBuffer(2) }));
    expect(output).toBe("audio");
    controller.interrupt();
    expect(controls).toContainEqual({ type: "cancel" });
    expect(interruptions()).toEqual(["user_interrupt_button"]);
    controller.interrupt();
    wire.dispatchEvent(new MessageEvent("message", { data: new ArrayBuffer(2) }));
    expect(output).toBe("");
    wire.dispatchEvent(new MessageEvent("message", { data: JSON.stringify({ type: "interrupted" }) }));
    expect(interruptions()).toEqual(["user_interrupt_button", "user_spoke_during_tts"]);
    wire.dispatchEvent(new MessageEvent("message", { data: new ArrayBuffer(2) }));
    expect(output).toBe("");
    wire.dispatchEvent(new MessageEvent("message", { data: JSON.stringify({
      type: "interrupted", provider_metadata: { reason: "client_cancel" }
    }) }));
    expect(interruptions()).toEqual(["user_interrupt_button", "user_spoke_during_tts"]);
    wire.dispatchEvent(new MessageEvent("message", { data: new ArrayBuffer(2) }));
    expect(output).toBe("audio");
    controller.dispose();
  });

  it("keeps a nonfatal failed turn usable for the next speech turn but tears down a fatal error", async () => {
    const { captured } = arm();
    const { stream, track } = trackedStream();
    const controller = createVoiceAgentUiController({
      serverUrl: "http://localhost:8080",
      getAudioStream: () => Promise.resolve(stream)
    });
    await controller.start();
    captured().hooks.onUserTranscript?.("unfinished old question", false);
    captured().hooks.onAgentTranscript?.("unfinished old reply", false);
    captured().hooks.onError?.({ type: "error", code: "turn_failed", message: "", fatal: false });
    expect(track.readyState).toBe("live");
    expect(captured().close).not.toHaveBeenCalled();
    expect(controller.getState().status).toBe("capturing");
    captured().hooks.onUserTranscript?.("next question", true);
    captured().hooks.onAgentTranscript?.("next answer", true);
    expect(controller.getState().transcript_final).toBe("next question");
    expect(controller.getState().spoken_response).toBe("next answer");
    captured().hooks.onError?.({ type: "error", code: "auth_expired", message: "", fatal: true });
    expect(track.readyState).toBe("ended");
    expect(captured().close).toHaveBeenCalled();
    expect(controller.getState().status).toBe("denied");
  });

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
    expect(controller.getState().denial?.reason_code).toBe("microphone_permission_denied");
    expect(openBrowserSession).not.toHaveBeenCalled();
  });

  it("never mints a session or opens the mic when the browser already blocks it", async () => {
    const getUserMedia = vi.fn();
    vi.stubGlobal("navigator", {
      mediaDevices: { getUserMedia },
      permissions: { query: () => Promise.resolve({ state: "denied" }) }
    });
    try {
      const controller = createVoiceAgentUiController({ serverUrl: "http://localhost:8080" });
      await controller.start();
      expect(controller.getState().status).toBe("denied");
      expect(getUserMedia).not.toHaveBeenCalled();
      expect(openBrowserSession).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

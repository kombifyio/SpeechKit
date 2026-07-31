import { describe, expect, it } from "vitest";
import { registerSpeechKitElements } from "../src/index.js";
import type { SpeechKitVoiceVisualizerElement } from "../src/elements/voice-visualizer.js";
import type { VoiceUiController } from "../src/core/controller.js";
import {
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceSessionState,
  type SpeechKitVoiceSurfaceContract
} from "../src/core/voice-surface.js";

const CONTRACT: SpeechKitVoiceSurfaceContract = {
  version: "speechkit.voice_surface.v1",
  surface: "floating_panel",
  mode: "dictation",
  capture_policy: "push_to_talk",
  transport: "gateway_http",
  capabilities: {
    dictation: true,
    assist: true,
    voice_agent: true,
    wakeword_local: false,
    tts: true,
    barge_in: true,
    local_pairing: false
  },
  session: {},
  provider: {}
};

function fakeController(): VoiceUiController & { emitCapture(): void } {
  let state: SpeechKitVoiceSessionState = createSpeechKitVoiceSessionState(CONTRACT);
  const listeners = new Set<(next: SpeechKitVoiceSessionState) => void>();
  return {
    contract: CONTRACT,
    start() {},
    stop() {},
    cancel() {},
    subscribe(listener) {
      listeners.add(listener);
      listener(state);
      return () => listeners.delete(listener);
    },
    getState() {
      return state;
    },
    emitCapture() {
      state = reduceSpeechKitVoiceEvent(state, {
        type: "voice.capture_started",
        surface: CONTRACT.surface,
        mode: CONTRACT.mode
      });
      for (const listener of listeners) listener(state);
    }
  };
}

describe("pre-registration property upgrade", () => {
  it("scoops own properties shadowing the accessors into the real setters", async () => {
    // SSR-host simulation: a property assigned BEFORE ./define ran lands as an
    // own data property that shadows the class accessor after upgrade (browser
    // spec behavior; happy-dom cannot replay the upgrade itself, so the
    // shadowing own property is planted directly and connectedCallback must
    // scoop it — src/core/upgrade.ts).
    registerSpeechKitElements();
    const el = document.createElement("speechkit-voice-visualizer") as SpeechKitVoiceVisualizerElement;
    const controller = fakeController();
    Object.defineProperty(el, "controller", {
      value: controller,
      writable: true,
      configurable: true,
      enumerable: true
    });
    // The own property shadows the accessor: reading it returns the raw value
    // without any subscription having happened yet.
    document.body.append(el);
    await Promise.resolve();

    controller.emitCapture();
    for (let i = 0; i < 4; i += 1) await Promise.resolve();
    expect(el.getAttribute("state")).toBe("listening");
    // The shadowing own property is gone; the accessor now answers.
    expect(Object.getOwnPropertyDescriptor(el, "controller")).toBeUndefined();
  });
});

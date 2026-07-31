import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { registerSpeechKitElements } from "../src/index.js";
import type { SpeechKitVoiceButtonElement } from "../src/elements/voice-button.js";
import type { SpeechKitVoiceOverlayElement } from "../src/elements/voice-overlay.js";
import type { SpeechKitVoiceProviderElement } from "../src/core/context.js";
import type { SpeechKitVoiceVisualizerElement } from "../src/elements/voice-visualizer.js";
import type { VoiceUiController } from "../src/core/controller.js";
import type { VoiceConsentAdapter } from "../src/elements/voice-consent.js";
import {
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceEvent,
  type SpeechKitVoiceEventType,
  type SpeechKitVoiceSessionState,
  type SpeechKitVoiceSurfaceContract
} from "../src/core/voice-surface.js";

function makeContract(voiceAgent: boolean): SpeechKitVoiceSurfaceContract {
  return {
    version: "speechkit.voice_surface.v1",
    surface: "floating_panel",
    mode: "dictation",
    capture_policy: "push_to_talk",
    transport: "gateway_http",
    capabilities: {
      dictation: true,
      assist: true,
      voice_agent: voiceAgent,
      wakeword_local: false,
      tts: true,
      barge_in: voiceAgent,
      local_pairing: false
    },
    session: {},
    provider: {}
  };
}

interface FakeController extends VoiceUiController {
  calls: string[];
  emit(type: SpeechKitVoiceEventType, payload?: Partial<SpeechKitVoiceEvent>): void;
}

function createFakeController(voiceAgent: boolean): FakeController {
  const contract = makeContract(voiceAgent);
  let state: SpeechKitVoiceSessionState = createSpeechKitVoiceSessionState(contract);
  const listeners = new Set<(next: SpeechKitVoiceSessionState) => void>();
  const calls: string[] = [];

  function emit(type: SpeechKitVoiceEventType, payload: Partial<SpeechKitVoiceEvent> = {}): void {
    const event: SpeechKitVoiceEvent = {
      type,
      surface: contract.surface,
      mode: contract.mode,
      ...payload
    };
    state = reduceSpeechKitVoiceEvent(state, event);
    for (const listener of listeners) listener(state);
  }

  return {
    contract,
    calls,
    emit,
    start(mode) {
      calls.push(`start:${mode ?? contract.mode}`);
      if (mode === "voice_agent" && !contract.capabilities.voice_agent) {
        emit("voice.denied", {
          mode: "voice_agent",
          error: {
            error_code: "voice_mode_unavailable",
            reason_code: "voice_agent_capability_disabled",
            capability: "speechkit.voiceagent.live",
            required_features: [],
            missing_features: ["speechkit.voiceagent.live"],
            retryable: false,
            user_guidance: {
              title: "This voice mode is not available",
              body: "Voice conversation is disabled for this account.",
              next_steps: ["Use dictation instead."]
            }
          }
        });
        return;
      }
      emit("voice.capture_started");
    },
    stop() {
      calls.push("stop");
      emit("voice.capture_stopped");
      emit("voice.transcript_final", { text: "done", final: true });
      emit("voice.tts_finished"); // settle to idle like the real controller
    },
    cancel() {
      calls.push("cancel");
      emit("voice.cancelled", { reason_code: "user_cancelled" });
    },
    interrupt() {
      calls.push("interrupt");
      emit("voice.barge_in", { reason_code: "user_spoke_during_tts" });
    },
    subscribe(listener) {
      listeners.add(listener);
      listener(state);
      return () => listeners.delete(listener);
    },
    getState() {
      return state;
    }
  };
}

async function flush(): Promise<void> {
  for (let i = 0; i < 6; i += 1) await Promise.resolve();
}

const grantedConsent: VoiceConsentAdapter = {
  read: () => "granted",
  write: () => {}
};

beforeAll(() => {
  registerSpeechKitElements();
});

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("<speechkit-voice-button>", () => {
  it("renders as a plain dictation button without the agent attribute", async () => {
    const el = document.createElement("speechkit-voice-button") as SpeechKitVoiceButtonElement;
    el.controller = createFakeController(true);
    document.body.append(el);
    await flush();
    const agent = el.shadowRoot!.querySelector(".agent") as HTMLButtonElement;
    expect(agent.hidden).toBe(true);
  });

  it("primary click starts dictation, then stops while capturing", async () => {
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-button") as SpeechKitVoiceButtonElement;
    el.controller = controller;
    document.body.append(el);
    await flush();
    const primary = el.shadowRoot!.querySelector(".primary") as HTMLButtonElement;
    primary.click();
    await flush();
    expect(controller.calls).toEqual(["start:dictation"]);
    expect(primary.dataset.status).toBe("capturing");
    primary.click();
    await flush();
    expect(controller.calls).toEqual(["start:dictation", "stop"]);
  });

  it("agent segment starts the voice agent when the capability is granted", async () => {
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-button") as SpeechKitVoiceButtonElement;
    el.setAttribute("agent", "");
    el.controller = controller;
    document.body.append(el);
    await flush();
    const agent = el.shadowRoot!.querySelector(".agent") as HTMLButtonElement;
    expect(agent.hidden).toBe(false);
    expect(agent.dataset.locked).toBeUndefined();
    agent.click();
    await flush();
    expect(controller.calls).toEqual(["start:voice_agent"]);
  });

  it("degrades to locked with denial guidance when voice_agent is not entitled", async () => {
    const controller = createFakeController(false);
    const el = document.createElement("speechkit-voice-button") as SpeechKitVoiceButtonElement;
    el.setAttribute("agent", "");
    el.controller = controller;
    document.body.append(el);
    await flush();
    const agent = el.shadowRoot!.querySelector(".agent") as HTMLButtonElement;
    expect(agent.dataset.locked).toBe("");
    const denialEvents: unknown[] = [];
    el.addEventListener("speechkit-denied", (event) => denialEvents.push((event as CustomEvent).detail));
    agent.click();
    await flush();
    expect(controller.calls).toEqual(["start:voice_agent"]);
    const popover = el.shadowRoot!.querySelector(".denial") as HTMLDivElement;
    expect(popover.hidden).toBe(false);
    expect(popover.textContent).toContain("This voice mode is not available");
    expect(denialEvents).toHaveLength(1);
  });
});

describe("<speechkit-voice-overlay>", () => {
  it("renders turns from the controller event stream and exits via stop", async () => {
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-overlay") as SpeechKitVoiceOverlayElement;
    el.consentAdapter = grantedConsent;
    el.controller = controller;
    document.body.append(el);
    el.show();
    await flush();
    expect(el.hasAttribute("open")).toBe(true);
    controller.emit("voice.capture_started");
    controller.emit("voice.transcript_draft", { text: "what is kombify" });
    controller.emit("voice.transcript_final", { text: "What is kombify?", final: true });
    controller.emit("voice.agent_turn", { output_text: "A platform.", final: true });
    await flush();
    const turns = el.shadowRoot!.querySelectorAll(".turn");
    expect(turns).toHaveLength(2);
    expect(turns[0]!.textContent).toContain("What is kombify?");
    expect(turns[1]!.textContent).toContain("A platform.");
    const exit = el.shadowRoot!.querySelector('[part="exit"]') as HTMLButtonElement;
    exit.click();
    await flush();
    expect(controller.calls).toContain("stop");
    expect(el.hasAttribute("open")).toBe(false);
  });

  it("gates on continuous consent before starting the session", async () => {
    const decisions: string[] = [];
    let consent = "unset" as "unset" | "granted";
    const adapter: VoiceConsentAdapter = {
      read: () => consent,
      write: (decision) => {
        decisions.push(decision);
        consent = decision === "granted" ? "granted" : "unset";
      }
    };
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-overlay") as SpeechKitVoiceOverlayElement;
    el.consentAdapter = adapter;
    el.controller = controller;
    document.body.append(el);
    el.show();
    await flush();
    const consentEl = el.shadowRoot!.querySelector("speechkit-voice-consent");
    expect(consentEl).not.toBeNull();
    expect(controller.calls).toEqual([]);
    const accept = consentEl!.shadowRoot!.querySelector(".accept") as HTMLButtonElement;
    accept.click();
    await flush();
    expect(decisions).toEqual(["granted"]);
    expect(controller.calls).toEqual(["start:voice_agent"]);
  });

  it("starts a fresh session and clears prior turns on reopen with granted consent", async () => {
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-overlay") as SpeechKitVoiceOverlayElement;
    el.consentAdapter = grantedConsent;
    el.controller = controller;
    document.body.append(el);
    el.show();
    await flush();
    expect(controller.calls).toEqual(["start:voice_agent"]);
    controller.emit("voice.agent_turn", { output_text: "First session.", final: true });
    await flush();
    expect(el.shadowRoot!.querySelectorAll(".turn")).toHaveLength(1);
    el.exit();
    await flush();
    el.show();
    await flush();
    expect(controller.calls).toEqual(["start:voice_agent", "stop", "start:voice_agent"]);
    expect(el.shadowRoot!.querySelectorAll(".turn")).toHaveLength(0);
    controller.emit("voice.transcript_final", { text: "Second session.", final: true });
    await flush();
    const turns = el.shadowRoot!.querySelectorAll(".turn");
    expect(turns).toHaveLength(1);
    expect(turns[0]!.textContent).toContain("Second session.");
  });

  it("marks the session ended when it settles while open and offers reconnect", async () => {
    const controller = createFakeController(true);
    const el = document.createElement("speechkit-voice-overlay") as SpeechKitVoiceOverlayElement;
    el.consentAdapter = grantedConsent;
    el.controller = controller;
    document.body.append(el);
    el.show();
    await flush();
    controller.emit("voice.capture_started");
    controller.emit("voice.tts_finished");
    await flush();
    const reconnect = el.shadowRoot!.querySelector('[part="reconnect"]') as HTMLButtonElement;
    expect(reconnect).not.toBeNull();
    reconnect.click();
    await flush();
    expect(controller.calls).toContain("start:voice_agent");
  });
});

describe("<speechkit-voice-provider>", () => {
  it("provides the controller to descendant elements via the context protocol", async () => {
    const controller = createFakeController(true);
    const provider = document.createElement(
      "speechkit-voice-provider"
    ) as SpeechKitVoiceProviderElement;
    document.body.append(provider);
    const visualizer = document.createElement(
      "speechkit-voice-visualizer"
    ) as SpeechKitVoiceVisualizerElement;
    provider.append(visualizer);
    provider.controller = controller;
    await flush();
    controller.emit("voice.capture_started");
    await flush();
    expect(visualizer.getAttribute("state")).toBe("listening");
  });
});

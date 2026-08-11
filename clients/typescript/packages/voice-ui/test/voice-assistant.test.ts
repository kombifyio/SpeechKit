import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { registerSpeechKitElements } from "../src/index.js";
import type { SpeechKitVoiceAssistantElement } from "../src/elements/voice-assistant.js";
import type { VoiceUiController } from "../src/core/controller.js";
import {
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceEvent,
  type SpeechKitVoiceEventType,
  type SpeechKitVoiceSessionState,
  type SpeechKitVoiceSurfaceContract
} from "../src/core/voice-surface.js";

function makeContract(): SpeechKitVoiceSurfaceContract {
  return {
    version: "speechkit.voice_surface.v1",
    surface: "floating_panel",
    mode: "voice_agent",
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
}

interface FakeController extends VoiceUiController {
  calls: string[];
  emit(type: SpeechKitVoiceEventType, payload?: Partial<SpeechKitVoiceEvent>): void;
  emitLevel(level: number, source: "input" | "output"): void;
  reset(): void;
}

function createFakeController(): FakeController {
  const contract = makeContract();
  let state: SpeechKitVoiceSessionState = createSpeechKitVoiceSessionState(contract);
  const listeners = new Set<(next: SpeechKitVoiceSessionState) => void>();
  const levelListeners = new Set<(level: number, source: "input" | "output") => void>();
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
    emitLevel(level, source) {
      for (const listener of levelListeners) listener(level, source);
    },
    reset() {
      state = createSpeechKitVoiceSessionState(contract);
      for (const listener of listeners) listener(state);
    },
    start(mode) {
      calls.push(`start:${mode ?? contract.mode}`);
      emit("voice.capture_started");
    },
    stop() {
      calls.push("stop");
      emit("voice.capture_stopped");
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
    },
    subscribeLevel(listener) {
      levelListeners.add(listener);
      return () => levelListeners.delete(listener);
    }
  };
}

async function flush(): Promise<void> {
  for (let i = 0; i < 6; i += 1) await Promise.resolve();
}

function makeElement(attrs: Record<string, string> = {}): SpeechKitVoiceAssistantElement {
  const element = document.createElement(
    "speechkit-voice-assistant"
  ) as SpeechKitVoiceAssistantElement;
  for (const [name, value] of Object.entries(attrs)) element.setAttribute(name, value);
  document.body.append(element);
  return element;
}

function wrap(element: SpeechKitVoiceAssistantElement): HTMLElement {
  const node = element.shadowRoot?.querySelector(".va");
  if (!(node instanceof HTMLElement)) throw new Error("assistant wrapper missing");
  return node;
}

beforeAll(() => {
  registerSpeechKitElements();
});

beforeEach(() => {
  document.body.replaceChildren();
});

describe("speechkit-voice-assistant", () => {
  it("renders the compact pill by default in idle, with the full orb layer stack", async () => {
    const element = makeElement();
    await flush();
    expect(wrap(element).dataset["status"]).toBe("idle");
    expect(wrap(element).dataset["aura"]).toBe("inactive");
    expect(wrap(element).dataset["frame"]).toBe("panel");
    expect(element.shadowRoot?.querySelector(".compact-shell")).not.toBeNull();
    for (const layer of ["glow", "sweep", "sweep-inner", "halo", "core", "spark"]) {
      expect(element.shadowRoot?.querySelector(`.orb .${layer}`)).not.toBeNull();
    }
  });

  it("renders the bare orb for size=orb", async () => {
    const element = makeElement({ size: "orb", transcript: "" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.agent_turn", { output_text: "Text that must not render.", final: true });
    await flush();
    expect(element.shadowRoot?.querySelector(".orb")).not.toBeNull();
    expect(element.shadowRoot?.querySelector(".compact-shell")).toBeNull();
    expect(element.shadowRoot?.querySelector(".expanded-shell")).toBeNull();
    expect(element.shadowRoot?.querySelector(".turn")).toBeNull();
    expect(element.shadowRoot?.textContent).not.toContain("must not render");
  });

  it("walks the session statuses and aura states from controller events", async () => {
    const element = makeElement();
    const controller = createFakeController();
    element.controller = controller;
    await flush();
    expect(wrap(element).dataset["status"]).toBe("idle");
    expect(wrap(element).dataset["aura"]).toBe("inactive");

    controller.emit("voice.capture_started");
    await flush();
    expect(wrap(element).dataset["status"]).toBe("capturing");
    expect(wrap(element).dataset["aura"]).toBe("listening");

    controller.emit("voice.capture_stopped");
    await flush();
    expect(wrap(element).dataset["status"]).toBe("processing");
    expect(wrap(element).dataset["aura"]).toBe("processing");

    controller.emit("voice.tts_started");
    await flush();
    expect(wrap(element).dataset["status"]).toBe("speaking");
    expect(wrap(element).dataset["aura"]).toBe("speaking");

    controller.emit("voice.tts_finished");
    await flush();
    expect(wrap(element).dataset["status"]).toBe("idle");
    expect(wrap(element).dataset["aura"]).toBe("inactive");
  });

  it("maps a denial to the error aura and honours host aura overrides", async () => {
    const element = makeElement({ size: "orb" });
    element.status = "denied";
    await flush();
    expect(wrap(element).dataset["aura"]).toBe("error");

    // Host FSM states the surface contract has no equivalent for.
    element.setAttribute("aura-state", "recovering");
    await flush();
    expect(wrap(element).dataset["aura"]).toBe("recovering");

    element.auraState = "settling";
    await flush();
    expect(wrap(element).dataset["aura"]).toBe("settling");

    element.setAttribute("aura-state", "bogus");
    element.auraState = undefined;
    await flush();
    expect(wrap(element).dataset["aura"]).toBe("error");
  });

  it("renders the expanded turn list with drafts, finals, and interrupted flags", async () => {
    const element = makeElement({ size: "expanded", transcript: "" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.capture_started");
    controller.emit("voice.transcript_draft", { text: "Hello" });
    controller.emit("voice.transcript_final", { text: "Hello there.", final: true });
    controller.emit("voice.agent_turn", { output_text: "Hi, how can", final: false });
    await flush();

    const turns = element.shadowRoot?.querySelectorAll(".turn");
    expect(turns?.length).toBe(2);
    expect(turns?.[0]?.getAttribute("data-role")).toBe("user");
    expect(turns?.[0]?.hasAttribute("data-draft")).toBe(false);
    expect(turns?.[1]?.getAttribute("data-role")).toBe("agent");
    expect(turns?.[1]?.hasAttribute("data-draft")).toBe(true);
    expect(turns?.[1]?.querySelector(".turn-text")?.textContent).toBe("Hi, how can");

    controller.emit("voice.tts_started");
    controller.emit("voice.barge_in", { reason_code: "user_spoke_during_tts" });
    await flush();
    const flagged = element.shadowRoot?.querySelector(".turn-flag");
    expect(flagged?.textContent).toBe("Interrupted");
  });

  it("hides transcript surfaces without the transcript attribute", async () => {
    const element = makeElement({ size: "expanded" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.agent_turn", { output_text: "Secret text", final: true });
    await flush();
    const turnsWrap = element.shadowRoot?.querySelector(".turns-wrap");
    expect(turnsWrap?.classList.contains("hidden")).toBe(true);
  });

  it("shows the last sentence in the compact line when transcript is on", async () => {
    const element = makeElement({ transcript: "" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.agent_turn", {
      output_text: "First sentence. Second sentence!",
      final: true
    });
    await flush();
    const line = element.shadowRoot?.querySelector(".compact-line");
    expect(line?.textContent).toContain("Second sentence!");
    expect(line?.textContent).not.toContain("First sentence.");
  });

  it("renders the watch face for frame=watch", async () => {
    const element = makeElement({ frame: "watch", transcript: "" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.capture_started");
    await flush();
    expect(element.shadowRoot?.querySelector(".watch-shell")).not.toBeNull();
    expect(element.shadowRoot?.querySelector(".watch-status")?.textContent).toBe("Listening");
  });

  it("rebuilds the skeleton when size changes", async () => {
    const element = makeElement();
    await flush();
    expect(element.shadowRoot?.querySelector(".compact-shell")).not.toBeNull();
    element.setAttribute("size", "expanded");
    await flush();
    expect(element.shadowRoot?.querySelector(".compact-shell")).toBeNull();
    expect(element.shadowRoot?.querySelector(".expanded-shell")).not.toBeNull();
  });

  it("renders a host-provided brand mark and no mark otherwise", async () => {
    const bare = makeElement();
    await flush();
    expect(bare.shadowRoot?.querySelector(".mark")).toBeNull();

    const marked = makeElement({ "mark-src": "https://example.test/mark.png" });
    await flush();
    const img = marked.shadowRoot?.querySelector(".mark img");
    expect(img?.getAttribute("src")).toBe("https://example.test/mark.png");
    expect(img?.getAttribute("aria-hidden")).toBe("true");
  });

  it("treats orb tap as barge-in only while speaking", async () => {
    const element = makeElement();
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.capture_started");
    await flush();

    const orb = element.shadowRoot?.querySelector(".orb") as HTMLButtonElement;
    orb.click();
    expect(controller.calls).not.toContain("interrupt");

    controller.emit("voice.tts_started");
    await flush();
    let interrupted = 0;
    element.addEventListener("speechkit-interrupt", () => {
      interrupted += 1;
    });
    orb.click();
    expect(controller.calls).toContain("interrupt");
    expect(interrupted).toBe(1);
  });

  it("localizes status labels via the locale attribute", async () => {
    const element = makeElement({ frame: "watch", locale: "de" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.capture_started");
    await flush();
    expect(element.shadowRoot?.querySelector(".watch-status")?.textContent).toBe("Hört zu");
  });

  it("supports presentational overrides without a controller", async () => {
    const element = makeElement({ size: "expanded", transcript: "" });
    element.status = "speaking";
    element.turns = [
      { role: "user", text: "Question?", final: true, interrupted: false },
      { role: "agent", text: "Answer.", final: false, interrupted: false }
    ];
    await flush();
    expect(wrap(element).dataset["status"]).toBe("speaking");
    expect(element.shadowRoot?.querySelectorAll(".turn").length).toBe(2);
  });

  it("drives --level from controller level emissions via the rAF loop", async () => {
    const element = makeElement();
    const controller = createFakeController();
    element.controller = controller;
    await flush();

    controller.emitLevel(1, "input");
    await new Promise((resolve) => setTimeout(resolve, 120));
    const level = Number.parseFloat(wrap(element).style.getPropertyValue("--level"));
    expect(level).toBeGreaterThan(0.3);
  });

  it("resets the turn list when the controller starts a fresh session", async () => {
    const element = makeElement({ size: "expanded", transcript: "" });
    const controller = createFakeController();
    element.controller = controller;
    controller.emit("voice.agent_turn", { output_text: "Old dialogue.", final: true });
    await flush();
    expect(element.shadowRoot?.querySelectorAll(".turn").length).toBe(1);

    controller.reset();
    controller.emit("voice.capture_started");
    await flush();
    expect(element.shadowRoot?.querySelectorAll(".turn").length).toBe(0);
  });
});

import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { registerSpeechKitElements } from "../src/index.js";
import type { SpeechKitVoiceAssistantElement } from "../src/elements/voice-assistant.js";
import type { VoiceUiController } from "../src/core/controller.js";
import {
  createSpeechKitVoiceSessionState,
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

beforeAll(() => {
  registerSpeechKitElements();
});

beforeEach(() => {
  document.body.replaceChildren();
});

describe("speechkit-voice-assistant variant attribute", () => {
  it("defaults to the aura variant and renders the orb layer stack", async () => {
    const element = makeElement();
    await flush();
    expect(element.variant).toBe("aura");
    expect(element.shadowRoot?.querySelector(".orb")).not.toBeNull();
    expect(element.shadowRoot?.querySelector(".wave-visual")).toBeNull();
    const wrap = element.shadowRoot?.querySelector(".va") as HTMLElement;
    expect(wrap.dataset["variant"]).toBe("aura");
  });

  it("treats unknown variant values as aura", async () => {
    const element = makeElement({ variant: "ring" });
    await flush();
    expect(element.variant).toBe("aura");
    expect(element.shadowRoot?.querySelector(".orb")).not.toBeNull();
  });

  it("reflects the variant property to the attribute", () => {
    const element = makeElement();
    element.variant = "waveform";
    expect(element.getAttribute("variant")).toBe("waveform");
  });

  it("renders the linear waveform strip instead of the orb in the compact pill", async () => {
    const element = makeElement({ variant: "waveform" });
    await flush();
    expect(element.shadowRoot?.querySelector(".orb")).toBeNull();
    const visual = element.shadowRoot?.querySelector(".wave-visual") as HTMLButtonElement;
    expect(visual).not.toBeNull();
    expect(visual.dataset["layout"]).toBe("linear");
    expect(visual.querySelector("canvas")).not.toBeNull();
    const wrap = element.shadowRoot?.querySelector(".va") as HTMLElement;
    expect(wrap.dataset["variant"]).toBe("waveform");
  });

  it("renders the radial waveform in round slots (orb, watch, expanded hero)", async () => {
    for (const attrs of [
      { variant: "waveform", size: "orb" },
      { variant: "waveform", frame: "watch" },
      { variant: "waveform", size: "expanded" }
    ]) {
      document.body.replaceChildren();
      const element = makeElement(attrs);
      await flush();
      const visual = element.shadowRoot?.querySelector(".wave-visual") as HTMLButtonElement;
      expect(visual, JSON.stringify(attrs)).not.toBeNull();
      expect(visual.dataset["layout"], JSON.stringify(attrs)).toBe("radial");
    }
  });

  it("keeps the linear strip in the keyboard bar", async () => {
    const element = makeElement({ variant: "waveform", frame: "keyboard" });
    await flush();
    const visual = element.shadowRoot?.querySelector(".wave-visual") as HTMLButtonElement;
    expect(visual.dataset["layout"]).toBe("linear");
  });

  it("has no mark slot in the waveform variant even with mark-src set", async () => {
    const element = makeElement({
      variant: "waveform",
      "mark-src": "https://example.test/mark.png"
    });
    await flush();
    expect(element.shadowRoot?.querySelector(".mark")).toBeNull();
  });

  it("rebuilds the skeleton when the variant changes", async () => {
    const element = makeElement();
    await flush();
    expect(element.shadowRoot?.querySelector(".orb")).not.toBeNull();
    element.setAttribute("variant", "waveform");
    await flush();
    expect(element.shadowRoot?.querySelector(".orb")).toBeNull();
    expect(element.shadowRoot?.querySelector(".wave-visual")).not.toBeNull();
  });

  it("treats a waveform tap as barge-in while speaking", async () => {
    const element = makeElement({ variant: "waveform" });
    const contract = makeContract();
    const calls: string[] = [];
    const controller: VoiceUiController = {
      contract,
      start: () => {},
      stop: () => {},
      cancel: () => {},
      interrupt: () => {
        calls.push("interrupt");
      },
      subscribe: () => () => {},
      getState: () => createSpeechKitVoiceSessionState(contract)
    };
    element.controller = controller;
    element.status = "speaking";
    await flush();

    let interrupted = 0;
    element.addEventListener("speechkit-interrupt", () => {
      interrupted += 1;
    });
    const visual = element.shadowRoot?.querySelector(".wave-visual") as HTMLButtonElement;
    visual.click();
    expect(calls).toContain("interrupt");
    expect(interrupted).toBe(1);

    element.status = "idle";
    await flush();
    visual.click();
    expect(calls.length).toBe(1);
    expect(interrupted).toBe(1);
  });
});

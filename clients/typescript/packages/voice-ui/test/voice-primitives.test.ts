import { afterEach, describe, expect, it, vi } from "vitest";
import { createVoiceConsentStore } from "../src/core/consent.js";
import { checkMicrophonePermission } from "../src/core/permission.js";
import { registerSpeechKitElements } from "../src/index.js";
import type { SpeechKitVoiceNoticeElement } from "../src/elements/voice-notice.js";
import type { VoiceUiController } from "../src/core/controller.js";
import {
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceSurfaceContract
} from "../src/core/voice-surface.js";

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe("voice consent store", () => {
  it("asks once, remembers on the device, and reports decisions to the host", () => {
    const onConsentChange = vi.fn();
    const first = createVoiceConsentStore({ surface: "web", onConsentChange });
    expect(first.read("continuous")).toBe("unset");
    first.write("granted", "continuous");

    const reloaded = createVoiceConsentStore({ surface: "web" });
    expect(reloaded.read("continuous")).toBe("granted");
    expect(onConsentChange).toHaveBeenCalledWith(expect.objectContaining({ decision: "granted" }));
  });

  it("re-asks after a consent version change and accepts an account-seeded record", () => {
    createVoiceConsentStore({ consentVersion: "1" }).write("granted", "continuous");
    const onConsentChange = vi.fn();
    const store = createVoiceConsentStore({ consentVersion: "2", onConsentChange });
    expect(store.read("continuous")).toBe("unset");

    store.setConsentRecord({
      decision: "granted",
      scopes: ["one_shot", "continuous"],
      consent_version: "2",
      decided_at: "2026-10-02T00:00:00.000Z"
    });
    expect(createVoiceConsentStore({ consentVersion: "2" }).read("continuous")).toBe("granted");
    expect(onConsentChange).not.toHaveBeenCalled();
  });
});

describe("checkMicrophonePermission", () => {
  it("answers from the Permissions API without opening the microphone", async () => {
    const getUserMedia = vi.fn();
    vi.stubGlobal("navigator", {
      mediaDevices: { getUserMedia },
      permissions: { query: () => Promise.resolve({ state: "denied" }) }
    });
    await expect(checkMicrophonePermission()).resolves.toBe("denied");
    expect(getUserMedia).not.toHaveBeenCalled();
  });
});

const CONTRACT: SpeechKitVoiceSurfaceContract = {
  version: "speechkit.voice_surface.v1",
  surface: "embed",
  mode: "voice_agent",
  capture_policy: "push_to_talk",
  transport: "voiceagent_ws_ticket",
  capabilities: {
    dictation: true,
    assist: false,
    voice_agent: true,
    wakeword_local: false,
    tts: true,
    barge_in: true,
    local_pairing: false
  },
  session: {},
  provider: {}
};

describe("speechkit-voice-notice", () => {
  it("shows a localized line without raw codes or URLs and retries through the controller", async () => {
    registerSpeechKitElements();
    const state = reduceSpeechKitVoiceEvent(createSpeechKitVoiceSessionState(CONTRACT), {
      type: "voice.denied",
      surface: "embed",
      mode: "voice_agent",
      error: {
        error_code: "microphone_permission_denied",
        reason_code: "microphone_permission_denied",
        capability: "speechkit.voiceagent.live",
        required_features: [],
        missing_features: [],
        retryable: true,
        user_guidance: {
          title: "microphone_permission_denied",
          body: "microphone_permission_denied · api.example.com/v1/speechkit/voiceagent/sessions",
          next_steps: ["api.example.com/v1/speechkit/voiceagent/sessions"]
        }
      }
    });
    const start = vi.fn();
    const controller: VoiceUiController = {
      contract: CONTRACT,
      start,
      stop() {},
      cancel() {},
      subscribe(listener) {
        listener(state);
        return () => {};
      },
      getState: () => state
    };
    const notice = document.createElement("speechkit-voice-notice") as SpeechKitVoiceNoticeElement;
    notice.controller = controller;
    document.body.append(notice);
    await Promise.resolve();

    const root = notice.shadowRoot as ShadowRoot;
    const line = root.querySelector('[part="message"]')?.textContent ?? "";
    expect(line.length).toBeGreaterThan(0);
    expect(line).not.toContain("microphone_permission_denied");
    expect(line).not.toContain("example.com");

    (root.querySelector('[part="details-toggle"]') as HTMLButtonElement).click();
    await Promise.resolve();
    expect(root.querySelector('[part="details"]')?.textContent ?? "").not.toContain("example.com");

    (root.querySelector('[part="retry"]') as HTMLButtonElement).click();
    expect(start).toHaveBeenCalledWith("voice_agent");
  });
});

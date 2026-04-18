import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  defaultOverlayState,
  defaultSettingsState,
  fetchOverlayState,
  fetchSettingsState,
  saveSettingsState,
  type SpeechKitSettingsState,
} from "@/lib/speechkit";

describe("speechkit frontend contract", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("defaults to tri-mode hotkeys with no active mode selected", () => {
    expect(defaultOverlayState.assistHotkey).toBe("ctrl+win");
    expect(defaultOverlayState.voiceAgentHotkey).toBe("ctrl+shift");
    expect(defaultOverlayState.activeMode).toBe("none");
    expect(defaultOverlayState.modeEnabled).toEqual({
      dictate: true,
      assist: true,
      voice_agent: true,
    });
    expect(defaultSettingsState.assistHotkey).toBe("ctrl+win");
    expect(defaultSettingsState.voiceAgentHotkey).toBe("ctrl+shift");
    expect(defaultSettingsState.voiceAgentRefinementPrompt).toBe("");
    expect(defaultSettingsState.activeMode).toBe("none");
    expect(defaultSettingsState.modeEnabled).toEqual({
      dictate: true,
      assist: true,
      voice_agent: true,
    });
  });

  it("maps legacy agent payloads onto the tri-mode settings shape", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          dictateHotkey: "win+alt",
          agentHotkey: "ctrl+shift+v",
          agentMode: "voice_agent",
          activeMode: "agent",
        }),
        { status: 200 },
      ),
    );

    const state = await fetchSettingsState();

    expect(state.dictateHotkey).toBe("win+alt");
    expect(state.assistHotkey).toBe("");
    expect(state.voiceAgentHotkey).toBe("ctrl+shift+v");
    expect(state.activeMode).toBe("voice_agent");
    expect(state.availableModes).toEqual({
      dictate: true,
      assist: false,
      voice_agent: true,
    });
  });

  it("normalizes the personal voice-agent refinement prompt from settings payloads", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          voiceAgentRefinementPrompt: "Refinement prompt",
        }),
        { status: 200 },
      ),
    );

    const state = await fetchSettingsState();

    expect(state.voiceAgentRefinementPrompt).toBe("Refinement prompt");
    expect(
      Object.prototype.hasOwnProperty.call(state, "voiceAgentFrameworkPrompt"),
    ).toBe(false);
  });

  it("coerces unavailable overlay modes back to none", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          dictateHotkey: "",
          assistHotkey: "",
          voiceAgentHotkey: "ctrl+shift+v",
          activeMode: "assist",
        }),
        { status: 200 },
      ),
    );

    const state = await fetchOverlayState();

    expect(state.activeMode).toBe("none");
    expect(state.availableModes).toEqual({
      dictate: false,
      assist: false,
      voice_agent: true,
    });
  });

  it("posts separate assist and voice-agent hotkeys while keeping a legacy fallback", async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ message: "Saved" }), { status: 200 }),
    );

    const nextState: SpeechKitSettingsState = {
      ...defaultSettingsState,
      dictateHotkey: "win+alt",
      assistHotkey: "",
      voiceAgentHotkey: "ctrl+shift+k",
      voiceAgentRefinementPrompt: "Refinement prompt",
      activeMode: "voice_agent",
    };

    await saveSettingsState(nextState);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [, init] = fetchMock.mock.calls[0];
    const body = init?.body;
    expect(body).toBeInstanceOf(URLSearchParams);

    const params = body as URLSearchParams;
    expect(params.get("dictate_hotkey")).toBe("win+alt");
    expect(params.get("assist_hotkey")).toBe("");
    expect(params.get("voice_agent_hotkey")).toBe("ctrl+shift+k");
    expect(params.get("voice_agent_refinement_prompt")).toBe("Refinement prompt");
    expect(params.has("voice_agent_framework_prompt")).toBe(false);
    expect(params.get("active_mode")).toBe("voice_agent");
    expect(params.get("agent_hotkey")).toBe("ctrl+shift+k");
    expect(params.get("agent_mode")).toBe("voice_agent");
  });

  it("uses the live overlay coordinates when saving movable overlay settings", async () => {
    fetchMock
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            ...defaultOverlayState,
            movable: true,
            positionFreeX: 884,
            positionFreeY: 412,
          }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ message: "Saved" }), { status: 200 }),
      );

    await saveSettingsState({
      ...defaultSettingsState,
      overlayMovable: true,
      overlayFreeX: 0,
      overlayFreeY: 0,
    });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/overlay/state");

    const [, init] = fetchMock.mock.calls[1];
    const body = init?.body;
    expect(body).toBeInstanceOf(URLSearchParams);

    const params = body as URLSearchParams;
    expect(params.get("overlay_free_x")).toBe("884");
    expect(params.get("overlay_free_y")).toBe("412");
  });
});

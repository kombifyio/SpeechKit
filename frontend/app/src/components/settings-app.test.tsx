import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import { SettingsApp } from "@/components/settings-app";
import type { ModelProfile, SpeechKitSettingsState } from "@/lib/speechkit";

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const {
  fetchSettingsStateMock,
  fetchModelProfilesMock,
  fetchOverlayStateMock,
  resetOverlayPositionMock,
  saveSettingsStateMock,
  saveProviderCredentialMock,
  clearProviderCredentialMock,
  testProviderCredentialMock,
  fetchAudioDevicesMock,
  setAudioDeviceMock,
  fetchDownloadCatalogMock,
  fetchDownloadJobsMock,
  startModelDownloadMock,
  cancelModelDownloadMock,
  selectDownloadedModelMock,
} = vi.hoisted(() => ({
  fetchSettingsStateMock: vi.fn<() => Promise<SpeechKitSettingsState>>(),
  fetchModelProfilesMock: vi.fn<() => Promise<ModelProfile[]>>(),
  fetchOverlayStateMock: vi.fn(),
  resetOverlayPositionMock: vi.fn<() => Promise<string>>(),
  saveSettingsStateMock:
    vi.fn<(state: SpeechKitSettingsState) => Promise<string>>(),
  saveProviderCredentialMock:
    vi.fn<
      (provider: string, secret: string) => Promise<{ message?: string }>
    >(),
  clearProviderCredentialMock:
    vi.fn<(provider: string) => Promise<{ message?: string }>>(),
  testProviderCredentialMock:
    vi.fn<
      (provider: string, secret: string) => Promise<{ message?: string }>
    >(),
  fetchAudioDevicesMock: vi.fn(),
  setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
  fetchDownloadCatalogMock: vi.fn(),
  fetchDownloadJobsMock: vi.fn(),
  startModelDownloadMock: vi.fn(),
  cancelModelDownloadMock: vi.fn(),
  selectDownloadedModelMock: vi.fn(),
}));

vi.mock("@/lib/speechkit", () => ({
  defaultSettingsState: {
    overlayEnabled: true,
    storeBackend: "sqlite",
    sqlitePath: "",
    postgresConfigured: false,
    postgresDSN: "",
    maxAudioStorageMB: 500,
    hfAvailable: true,
    hfEnabled: false,
    hotkey: "win+alt",
    dictateHotkey: "win+alt",
    assistHotkey: "ctrl+win",
    voiceAgentHotkey: "ctrl+shift",
    dictateHotkeyBehavior: "push_to_talk",
    assistHotkeyBehavior: "push_to_talk",
    voiceAgentHotkeyBehavior: "push_to_talk",
    voiceAgentCloseBehavior: "continue",
    voiceAgentRefinementPrompt: "",
    autoStartOnLaunch: false,
    agentHotkey: "ctrl+win",
    agentMode: "assist",
    activeMode: "none",
    modeEnabled: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    availableModes: {
      dictate: true,
      assist: true,
      voice_agent: true,
    },
    hfModel: "openai/whisper-large-v3-turbo",
    visualizer: "pill",
    design: "default",
    overlayPosition: "top",
    overlayMovable: false,
    overlayFreeX: 0,
    overlayFreeY: 0,
    vocabularyDictionary: "",
    saveAudio: true,
    audioRetentionDays: 7,
    selectedAudioDeviceId: "",
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: "none",
    activeProfiles: {},
    modelSelections: {
      dictate: { primaryProfileId: "", fallbackProfileId: "" },
      assist: { primaryProfileId: "", fallbackProfileId: "" },
      voice_agent: { primaryProfileId: "", fallbackProfileId: "" },
    },
    providerCredentials: {
      huggingface: {
        provider: "huggingface",
        label: "Hugging Face",
        envName: "HF_TOKEN",
        available: true,
        hasStoredSecret: false,
        source: "none",
      },
    },
  } satisfies SpeechKitSettingsState,
  fetchSettingsState: fetchSettingsStateMock,
  fetchModelProfiles: fetchModelProfilesMock,
  fetchOverlayState: fetchOverlayStateMock,
  resetOverlayPosition: resetOverlayPositionMock,
  saveSettingsState: saveSettingsStateMock,
  saveProviderCredential: saveProviderCredentialMock,
  clearProviderCredential: clearProviderCredentialMock,
  testProviderCredential: testProviderCredentialMock,
  fetchAudioDevices: fetchAudioDevicesMock,
  setAudioDevice: setAudioDeviceMock,
  fetchDownloadCatalog: fetchDownloadCatalogMock,
  fetchDownloadJobs: fetchDownloadJobsMock,
  startModelDownload: startModelDownloadMock,
  cancelModelDownload: cancelModelDownloadMock,
  selectDownloadedModel: selectDownloadedModelMock,
}));

vi.mock("@/components/ui/mic-selector", () => ({
  MicSelector: ({
    value,
    onValueChange,
  }: {
    value?: string;
    onValueChange?: (deviceId: string) => void;
  }) => (
    <button
      type="button"
      aria-label={`Microphone ${value || "Studio Mic"}`}
      onClick={() => {
        void setAudioDeviceMock("mic-1");
        onValueChange?.("mic-1");
      }}
    >
      Microphone {value || "Studio Mic"}
    </button>
  ),
}));

const baseSettings: SpeechKitSettingsState = {
  overlayEnabled: true,
  storeBackend: "sqlite",
  sqlitePath: "C:/Users/testuser/AppData/Roaming/SpeechKit/feedback.db",
  postgresConfigured: false,
  postgresDSN: "",
  maxAudioStorageMB: 500,
  hfAvailable: true,
  hfEnabled: false,
  hotkey: "win+alt",
  dictateHotkey: "win+alt",
  assistHotkey: "ctrl+win",
  voiceAgentHotkey: "ctrl+shift",
  dictateHotkeyBehavior: "push_to_talk",
  assistHotkeyBehavior: "push_to_talk",
  voiceAgentHotkeyBehavior: "push_to_talk",
  voiceAgentCloseBehavior: "continue",
  voiceAgentRefinementPrompt: "",
  autoStartOnLaunch: false,
  agentHotkey: "ctrl+win",
  agentMode: "assist",
  activeMode: "none",
  modeEnabled: {
    dictate: true,
    assist: true,
    voice_agent: true,
  },
  availableModes: {
    dictate: true,
    assist: true,
    voice_agent: true,
  },
  hfModel: "openai/whisper-large-v3-turbo",
  visualizer: "pill",
  design: "default",
  overlayPosition: "top",
  overlayMovable: false,
  overlayFreeX: 0,
  overlayFreeY: 0,
  vocabularyDictionary: "",
  saveAudio: true,
  audioRetentionDays: 7,
  selectedAudioDeviceId: "mic-1",
  hfHasUserToken: false,
  hfHasInstallToken: false,
  hfTokenSource: "none",
  activeProfiles: { stt: "stt-local" },
  modelSelections: {
    dictate: { primaryProfileId: "stt-local", fallbackProfileId: "" },
    assist: { primaryProfileId: "", fallbackProfileId: "" },
    voice_agent: { primaryProfileId: "", fallbackProfileId: "" },
  },
  profiles: [
    {
      id: "stt-local",
      modality: "stt",
      name: "Qwen/Qwen3-ASR-1.7B",
      executionMode: "local",
      description: "Default local STT profile",
    },
  ],
  providerCredentials: {
    huggingface: {
      provider: "huggingface",
      label: "Hugging Face",
      envName: "HF_TOKEN",
      available: true,
      hasStoredSecret: false,
      source: "none",
    },
  },
};

describe("SettingsApp", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    fetchSettingsStateMock.mockReset();
    fetchModelProfilesMock.mockReset();
    saveSettingsStateMock.mockReset();
    fetchOverlayStateMock.mockReset();
    resetOverlayPositionMock.mockReset();
    saveProviderCredentialMock.mockReset();
    clearProviderCredentialMock.mockReset();
    testProviderCredentialMock.mockReset();
    fetchAudioDevicesMock.mockReset();
    setAudioDeviceMock.mockReset();
    fetchDownloadCatalogMock.mockReset();
    fetchDownloadJobsMock.mockReset();
    startModelDownloadMock.mockReset();
    cancelModelDownloadMock.mockReset();
    selectDownloadedModelMock.mockReset();

    fetchModelProfilesMock.mockResolvedValue([]);
    fetchAudioDevicesMock.mockResolvedValue({
      devices: [
        {
          deviceId: "mic-1",
          label: "Studio Mic",
          groupId: "group-1",
          isDefault: true,
        },
        { deviceId: "mic-2", label: "Backup Mic", groupId: "group-2" },
      ],
      selectedDeviceId: "mic-1",
    });
    saveSettingsStateMock.mockResolvedValue("Gespeichert");
    fetchOverlayStateMock.mockResolvedValue({
      state: "idle",
      phase: "idle",
      text: "",
      level: 0,
      visible: true,
      visualizer: "pill",
      design: "default",
      hotkey: "win+alt",
      dictateHotkey: "win+alt",
      assistHotkey: "ctrl+win",
      voiceAgentHotkey: "ctrl+shift",
      dictateHotkeyBehavior: "push_to_talk",
      assistHotkeyBehavior: "push_to_talk",
      voiceAgentHotkeyBehavior: "toggle",
      modeEnabled: { dictate: true, assist: true, voice_agent: true },
      availableModes: { dictate: true, assist: true, voice_agent: true },
      agentHotkey: "ctrl+win",
      activeMode: "none",
      position: "top",
      movable: true,
      positionFreeX: 884,
      positionFreeY: 412,
      lastTranscription: "",
      quickNoteMode: false,
      selectedAudioDeviceId: "mic-1",
      activeProfiles: {},
    });
    resetOverlayPositionMock.mockResolvedValue("Saved");
    saveProviderCredentialMock.mockResolvedValue({ message: "Saved" });
    clearProviderCredentialMock.mockResolvedValue({ message: "Cleared" });
    testProviderCredentialMock.mockResolvedValue({ message: "Key valid" });
    setAudioDeviceMock.mockResolvedValue("Selected");
    fetchDownloadCatalogMock.mockResolvedValue([]);
    fetchDownloadJobsMock.mockResolvedValue([]);
    startModelDownloadMock.mockResolvedValue({
      id: "dl-1",
      modelId: "test",
      profileId: "test",
      status: "pending",
      progress: 0,
      bytesDone: 0,
      totalBytes: 0,
      statusText: "Starting…",
    });
    cancelModelDownloadMock.mockResolvedValue(undefined);
    selectDownloadedModelMock.mockResolvedValue({ message: "Selected" });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps model selection available while hugging face inference is off", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: "openai/whisper-large-v3",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    const modelsSection = await screen.findByText("Model setup");
    expect(modelsSection).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Transcribe" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "TTS" }),
    ).not.toBeInTheDocument();
  });

  it("saves model changes even while hugging face inference is off", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: "openai/whisper-large-v3",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    await screen.findByText("Model setup");
    expect(screen.getByText("Primary")).toBeInTheDocument();
  });

  it("renders model descriptions for live-switchable profiles", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.builtin.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Local Built-in)",
          executionMode: "local",
          source: "Local Built-in",
          description: "SpeechKit-managed local Gemma runtime.",
        },
        {
          id: "assist.ollama.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Ollama)",
          executionMode: "ollama_local",
          source: "Local Provider",
          description:
            "Laptop-friendly local model for summaries and quick actions.",
        },
      ],
      activeProfiles: { assist: "assist.ollama.gemma4-e4b" },
    });

    render(<SettingsApp />);

    const assistButtons = await screen.findAllByRole("button", {
      name: "Assist",
    });
    fireEvent.click(assistButtons[0]);

    expect(
      await screen.findByText(/laptop-friendly local model/i),
    ).toBeInTheDocument();
    expect(screen.getByText("built-in")).toBeInTheDocument();
    expect(screen.getByText("Local Built-in")).toBeInTheDocument();
    expect(screen.getByText("provider")).toBeInTheDocument();
    expect(screen.getByText("Local Provider")).toBeInTheDocument();
  });

  it("keeps general settings separate from per-mode controls", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    await screen.findByText("Storage");
    expect(screen.queryByText("Mode")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Active mode Assist" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Active mode None" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Dictate hotkey")).not.toBeInTheDocument();
    expect(screen.queryByText("Assist hotkey")).not.toBeInTheDocument();
    expect(screen.queryByText("Voice Agent hotkey")).not.toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Auto-start on app launch" }),
    ).toHaveAttribute("aria-checked", "false");

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(screen.getByText("Dictate hotkey")).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Enable Dictate hotkey" }),
    ).toHaveAttribute("aria-checked", "true");

    fireEvent.click(screen.getByRole("button", { name: "Dictate hotkey Ctrl + Win" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: "ctrl+win",
          hotkey: "ctrl+win",
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Assist" }));
    expect(screen.getByText("Assist hotkey")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Assist hotkey Ctrl + Shift" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkey: "ctrl+shift",
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent" }));
    expect(screen.getByText("Voice Agent hotkey")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("switch", { name: "Enable Voice Agent hotkey" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modeEnabled: expect.objectContaining({
            voice_agent: false,
          }),
          availableModes: expect.objectContaining({
            voice_agent: false,
          }),
          voiceAgentHotkey: "ctrl+shift",
        }),
      ),
    );
  });

  it("supports an optional third key for each two-key hotkey base", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      assistHotkey: "ctrl+win+j",
      voiceAgentHotkey: "ctrl+shift+k",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist" }));
    await screen.findByText("Assist hotkey");

    expect(
      screen.getByRole("button", { name: "Assist hotkey Ctrl + Win" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Assist hotkey suffix")).toHaveValue("j");

    fireEvent.click(screen.getByRole("button", { name: "Transcribe" }));

    fireEvent.change(screen.getByLabelText("Dictate hotkey suffix"), {
      target: { value: "d" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: "win+alt+d",
          hotkey: "win+alt+d",
        }),
      ),
    );
  });

  it("saves per-mode hotkey trigger behavior independently", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist" }));
    await screen.findByText("Assist hotkey");

    fireEvent.click(
      screen.getByRole("button", { name: "Assist hotkey Toggle on press" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkeyBehavior: "toggle",
          dictateHotkeyBehavior: "push_to_talk",
          voiceAgentHotkeyBehavior: "push_to_talk",
        }),
      ),
    );
  });

  it("saves the voice agent close behavior independently", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="realtime_voice" />);

    fireEvent.click(await screen.findByRole("button", { name: "End chat on close" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentCloseBehavior: "new_chat",
        }),
      ),
    );
  });

  it("renders only the personal refinement prompt and saves it independently", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      voiceAgentRefinementPrompt: "Address the user by first name.",
    });

    render(<SettingsApp initialTab="realtime_voice" />);

    expect(
      screen.queryByLabelText("Voice Agent framework prompt"),
    ).not.toBeInTheDocument();

    const refinementPrompt = await screen.findByLabelText(
      "Voice Agent personal refinement prompt",
    );

    expect(refinementPrompt).toHaveValue("Address the user by first name.");

    fireEvent.change(refinementPrompt, {
      target: { value: "Keep answers concise and warm." },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentRefinementPrompt: "Keep answers concise and warm.",
        }),
      ),
    );
  });

  it("saves the app auto-start setting from general settings", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="general" />);

    fireEvent.click(
      await screen.findByRole("switch", { name: "Auto-start on app launch" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          autoStartOnLaunch: true,
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent" }));
    expect(
      screen.queryByRole("switch", { name: "Auto-start on app launch" }),
    ).not.toBeInTheDocument();
  });

  it("reloads settings and model profiles when the dashboard is shown again", async () => {
    fetchSettingsStateMock.mockResolvedValueOnce(baseSettings);
    fetchSettingsStateMock.mockResolvedValueOnce({
      ...baseSettings,
      dictateHotkey: "win+alt+d",
    });
    fetchModelProfilesMock.mockResolvedValue([]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));
    await screen.findByText("Dictate hotkey");
    expect(fetchSettingsStateMock).toHaveBeenCalledTimes(1);
    expect(fetchModelProfilesMock).toHaveBeenCalledTimes(1);

    window.dispatchEvent(new CustomEvent("speechkit:dashboard-show"));

    await waitFor(() =>
      expect(fetchSettingsStateMock).toHaveBeenCalledTimes(2),
    );
    await waitFor(() =>
      expect(fetchModelProfilesMock).toHaveBeenCalledTimes(2),
    );
    expect(await screen.findByLabelText("Dictate hotkey suffix")).toHaveValue(
      "d",
    );
  });

  it("renders model cards from settings state when the profile fetch fails", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });
    fetchModelProfilesMock.mockRejectedValue(new Error("profiles unavailable"));

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
  });

  it("blocks duplicate two-key bases across the three modes", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);
    saveSettingsStateMock.mockRejectedValueOnce(
      new Error("Each mode needs its own two-key base."),
    );

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist" }));
    await screen.findByText("Assist hotkey");
    fireEvent.click(
      screen.getByRole("button", { name: "Assist hotkey Win + Alt" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkey: "win+alt",
        }),
      ),
    );
    expect(
      await screen.findByText("Each mode needs its own two-key base."),
    ).toBeInTheDocument();
  });

  it("shows the selected microphone from the desktop selector", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    const micButton = await screen.findByRole("button", {
      name: /microphone studio mic/i,
    });
    expect(micButton).toBeInTheDocument();
  });

  it("saves vocabulary dictionary changes", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      vocabularyDictionary: "kombi fire => Kombify",
    });

    render(<SettingsApp />);

    const input = await screen.findByLabelText("Vocabulary dictionary");
    fireEvent.change(input, {
      target: { value: "kombi fire => Kombify\nAcmeOS" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          vocabularyDictionary: "kombi fire => Kombify\nAcmeOS",
        }),
      ),
    );
  });

  it("saves overlay design changes", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    const button = await screen.findByRole("button", { name: "kombify" });

    fireEvent.click(button);

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ design: "kombify" }),
      ),
    );
  });

  it("allows enabling movable overlay while keeping the position chips", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(
      await screen.findByRole("switch", { name: "Movable overlay" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          overlayMovable: true,
          overlayPosition: "top",
        }),
      ),
    );

    expect(
      screen.getByText(/drag the center bubble inside the pill panel/i),
    ).toBeInTheDocument();
  });

  it("saves the current live overlay spot from the runtime state", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      overlayMovable: true,
    });

    render(<SettingsApp />);

    expect(
      await screen.findByRole("button", { name: "Save current spot" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save current spot" }));

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          overlayMovable: true,
          overlayFreeX: 884,
          overlayFreeY: 412,
        }),
      ),
    );
  });

  it("resets the saved overlay spot through the dedicated action", async () => {
    fetchSettingsStateMock
      .mockResolvedValueOnce({
        ...baseSettings,
        overlayMovable: true,
        overlayFreeX: 884,
        overlayFreeY: 412,
      })
      .mockResolvedValueOnce({
        ...baseSettings,
        overlayMovable: true,
        overlayFreeX: 0,
        overlayFreeY: 0,
      });

    render(<SettingsApp />);

    const button = await screen.findByRole("button", {
      name: "Reset saved spot",
    });
    expect(button).toBeEnabled();

    fireEvent.click(button);

    await waitFor(() =>
      expect(resetOverlayPositionMock).toHaveBeenCalledTimes(1),
    );
    await waitFor(() =>
      expect(fetchSettingsStateMock).toHaveBeenCalledTimes(2),
    );
  });

  it("saves local audio storage preferences", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    const saveAudioToggle = await screen.findByRole("switch", {
      name: "Save raw audio locally",
    });
    fireEvent.click(saveAudioToggle);

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ saveAudio: false }),
      ),
    );

    const retentionSelect = await screen.findByLabelText("Audio retention");
    fireEvent.change(retentionSelect, { target: { value: "30" } });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ audioRetentionDays: 30 }),
      ),
    );
  });

  it("persists the postgres connection string once the backend is configured locally", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "PostgreSQL" }));

    fireEvent.change(
      await screen.findByLabelText("PostgreSQL connection string"),
      {
        target: {
          value:
            "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable",
        },
      },
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: "postgres",
          postgresDSN:
            "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable",
        }),
      ),
    );
  });

  it("shows backend-specific storage copy and validation hints", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(await screen.findByLabelText("SQLite path")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("PostgreSQL connection string"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "PostgreSQL" }));

    expect(
      await screen.findByLabelText("PostgreSQL connection string"),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("SQLite path")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        /add a postgresql connection string before switching the metadata backend/i,
      ),
    ).toBeInTheDocument();
  });

  it("does not persist the postgres backend switch until a connection string is present", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "PostgreSQL" }));

    await screen.findByLabelText("PostgreSQL connection string");
    expect(saveSettingsStateMock).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("PostgreSQL connection string"), {
      target: {
        value:
          "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable",
      },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: "postgres",
          postgresDSN:
            "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable",
        }),
      ),
    );
  });

  it("persists the local audio storage cap", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    const input = await screen.findByLabelText("Max local audio storage (MB)");
    fireEvent.change(input, {
      target: { value: "1024" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ maxAudioStorageMB: 1024 }),
      ),
    );
  });

  it("shows stored token status for the provider", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "install",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(
      await screen.findByText(/install token active/i),
    ).toBeInTheDocument();
  });

  it("limits model choices to four cards per modality while keeping the active model visible", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    const assistProfiles = Array.from({ length: 6 }, (_, index) => ({
      id: `assist-model-${index + 1}`,
      modality: "assist" as const,
      name: `Assist Model ${index + 1}`,
      executionMode: "groq_api" as const,
      description: `Assist profile ${index + 1}`,
    }));

    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: assistProfiles,
      activeProfiles: { assist: "assist-model-6" },
      providerCredentials: {
        groq: {
          provider: "groq",
          label: "Groq",
          envName: "GROQ_API_KEY",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist",
    });
    fireEvent.click(assistNavButtons[0]);

    expect(await screen.findAllByText("Assist Model 1")).toHaveLength(3);
    expect(screen.getAllByText("Assist Model 2")).toHaveLength(3);
    expect(screen.getAllByText("Assist Model 3")).toHaveLength(3);
    expect(screen.getAllByText("Assist Model 6")).toHaveLength(3);
    expect(screen.getAllByText("Assist Model 4")).toHaveLength(2);
    expect(screen.getAllByText("Assist Model 5")).toHaveLength(2);
  });

  it("shows inline key entry on the model card when a provider key is missing", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.groq.llama-3.1-8b",
          modality: "assist",
          name: "LLaMA 3.1 8B (Groq)",
          executionMode: "groq_api",
          description: "Fast cloud LLM",
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        groq: {
          provider: "groq",
          label: "Groq",
          envName: "GROQ_API_KEY",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist",
    });
    fireEvent.click(assistNavButtons[0]);

    const keyInput = await screen.findByLabelText(
      "LLaMA 3.1 8B (Groq) Groq key",
    );
    expect(keyInput).toBeInTheDocument();

    fireEvent.change(keyInput, {
      target: { value: "groq_user_token_123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save key" }));

    await waitFor(() =>
      expect(saveProviderCredentialMock).toHaveBeenCalledWith(
        "groq",
        "groq_user_token_123",
      ),
    );
  });

  it("keeps Hugging Face token setup directly on the model card", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Add Hugging Face token")).toBeInTheDocument();
    expect(
      screen.getByLabelText(
        "Whisper Large v3 (Hugging Face) Hugging Face token",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Save token" }),
    ).toBeInTheDocument();
  });

  it("keeps Hugging Face-backed assist models selectable from the Assist tab", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.hf.qwen3-coder",
          modality: "assist",
          name: "Qwen 3 Coder (Hugging Face)",
          executionMode: "hf_inference",
          description: "Managed coding assistant profile",
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist",
    });
    fireEvent.click(assistNavButtons[0]);

    expect(
      (await screen.findAllByText("Qwen 3 Coder (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/user token active/i)).toBeInTheDocument();
    expect(screen.getByLabelText("Primary model")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Use model" }),
    ).not.toBeInTheDocument();
  });

  it("does not render the duplicated connected providers section", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText("Connected providers")).not.toBeInTheDocument();
  });

  it("clears a stored provider credential explicitly", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));
    fireEvent.click(await screen.findByRole("button", { name: "Clear" }));

    await waitFor(() =>
      expect(clearProviderCredentialMock).toHaveBeenCalledWith("huggingface"),
    );
  });

  it("renames the STT settings tab to Transcribe", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(
      await screen.findByRole("button", { name: "Transcribe" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "STT" }),
    ).not.toBeInTheDocument();
  });

  it("allows switching between available local whisper downloads", async () => {
    const initialSettings: SpeechKitSettingsState = {
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local)",
          executionMode: "local",
          description: "Offline Windows dictation with Whisper.cpp.",
        },
      ],
      activeProfiles: { stt: "stt.local.whispercpp" },
    };
    fetchSettingsStateMock
      .mockResolvedValueOnce(initialSettings)
      .mockResolvedValueOnce(initialSettings);
    fetchDownloadCatalogMock
      .mockResolvedValueOnce([
        {
          id: "whisper.ggml-small",
          profileId: "stt.local.whispercpp",
          name: "Whisper Small Multilingual (466 MB)",
          description: "Small local model",
          sizeLabel: "466 MB",
          sizeBytes: 484264096,
          kind: "http",
          license: "mit",
          available: true,
          selected: true,
        },
        {
          id: "whisper.ggml-large-v3-turbo",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 Turbo",
          description: "Turbo local model",
          sizeLabel: "~1.6 GB",
          sizeBytes: 1624555275,
          kind: "http",
          license: "mit",
          available: true,
          recommended: true,
          selected: false,
        },
        {
          id: "whisper.ggml-large-v3",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 (Open Source)",
          description: "Large local model",
          sizeLabel: "~3.1 GB",
          sizeBytes: 3100000000,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: "whisper.ggml-small",
          profileId: "stt.local.whispercpp",
          name: "Whisper Small Multilingual (466 MB)",
          description: "Small local model",
          sizeLabel: "466 MB",
          sizeBytes: 484264096,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
        {
          id: "whisper.ggml-large-v3-turbo",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 Turbo",
          description: "Turbo local model",
          sizeLabel: "~1.6 GB",
          sizeBytes: 1624555275,
          kind: "http",
          license: "mit",
          available: true,
          recommended: true,
          selected: true,
        },
        {
          id: "whisper.ggml-large-v3",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 (Open Source)",
          description: "Large local model",
          sizeLabel: "~3.1 GB",
          sizeBytes: 3100000000,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
      ]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    const switchButton = await screen.findByRole("button", {
      name: "Use Whisper Large v3 Turbo",
    });
    fireEvent.click(switchButton);

    await waitFor(() =>
      expect(selectDownloadedModelMock).toHaveBeenCalledWith(
        "whisper.ggml-large-v3-turbo",
      ),
    );
    expect(
      await screen.findByLabelText("Use Whisper Small Multilingual (466 MB)"),
    ).toBeInTheDocument();
    expect(screen.getByText("Selected on this device")).toBeInTheDocument();
    expect(screen.getByText("recommended")).toBeInTheDocument();
  });

  it("shows a runtime-missing warning instead of offering local model switching when whisper runtime is absent", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local)",
          executionMode: "local",
          description: "Offline Windows dictation with Whisper.cpp.",
        },
      ],
      activeProfiles: {},
    });
    fetchDownloadCatalogMock.mockResolvedValue([
      {
        id: "whisper.ggml-large-v3-turbo",
        profileId: "stt.local.whispercpp",
        name: "Whisper Large v3 Turbo",
        description: "Turbo local model",
        sizeLabel: "~1.6 GB",
        sizeBytes: 1624555275,
        kind: "http",
        license: "mit",
        available: true,
        selected: false,
        recommended: true,
        runtimeReady: false,
        runtimeProblem: "Local runtime missing: whisper-server binary missing.",
      },
    ]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe" }));

    expect(
      await screen.findAllByText(
        /local runtime missing: whisper-server binary missing/i,
      ),
    ).toHaveLength(2);
    expect(
      screen.queryByRole("button", { name: "Use Whisper Large v3 Turbo" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the mode settings layout scrollable and flattens the top control chrome", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);
    fetchModelProfilesMock.mockResolvedValue(baseSettings.profiles ?? []);

    render(<SettingsApp initialTab="assist" />);

    await screen.findByText("Assist hotkey");

    expect(screen.getByTestId("settings-layout")).toHaveClass("min-h-0");
    expect(screen.getByTestId("settings-scroll-region")).toHaveClass(
      "overflow-y-auto",
    );
    expect(screen.getByTestId("assist-mode-controls")).not.toHaveClass(
      "sk-panel",
    );
  });
});

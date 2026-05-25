import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

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
  updateProviderIntegrationMock,
  fetchAudioDevicesMock,
  setAudioDeviceMock,
  fetchAPIV1DictionaryMock,
  importAPIV1DictionaryMock,
  fetchDownloadCatalogMock,
  fetchDownloadJobsMock,
  startModelDownloadMock,
  cancelModelDownloadMock,
  selectDownloadedModelMock,
  openFileDialogMock,
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
  updateProviderIntegrationMock:
    vi.fn<
      (provider: string, enabled: boolean) => Promise<{ message?: string }>
    >(),
  fetchAudioDevicesMock: vi.fn(),
  setAudioDeviceMock: vi.fn<(deviceId: string) => Promise<string>>(),
  fetchAPIV1DictionaryMock: vi.fn(),
  importAPIV1DictionaryMock: vi.fn(),
  fetchDownloadCatalogMock: vi.fn(),
  fetchDownloadJobsMock: vi.fn(),
  startModelDownloadMock: vi.fn(),
  cancelModelDownloadMock: vi.fn(),
  selectDownloadedModelMock: vi.fn(),
  openFileDialogMock: vi.fn(),
}));

export {
  fetchSettingsStateMock,
  fetchModelProfilesMock,
  fetchOverlayStateMock,
  resetOverlayPositionMock,
  saveSettingsStateMock,
  saveProviderCredentialMock,
  clearProviderCredentialMock,
  testProviderCredentialMock,
  updateProviderIntegrationMock,
  fetchAudioDevicesMock,
  setAudioDeviceMock,
  fetchAPIV1DictionaryMock,
  importAPIV1DictionaryMock,
  fetchDownloadCatalogMock,
  fetchDownloadJobsMock,
  startModelDownloadMock,
  cancelModelDownloadMock,
  selectDownloadedModelMock,
  openFileDialogMock,
};

vi.mock("@wailsio/runtime", () => ({
  Dialogs: {
    OpenFile: openFileDialogMock,
  },
}));

vi.mock("@/lib/speechkit", () => ({
  builtInPrimaryModelSelections: {
    dictate: {
      primaryProfileId: "stt.local.whispercpp",
      fallbackProfileId: "",
    },
    assist: {
      primaryProfileId: "assist.builtin.gemma4-e4b",
      fallbackProfileId: "",
    },
    voice_agent: {
      primaryProfileId: "realtime.google.gemini-native-audio",
      fallbackProfileId: "",
    },
  },
  defaultSettingsState: {
    overlayEnabled: true,
    storeBackend: "sqlite",
    sqlitePath: "",
    postgresConfigured: false,
    postgresDSN: "",
    maxAudioStorageMB: 500,
    hfAvailable: true,
    hfEnabled: false,
    hotkey: "ctrl+win",
    dictateHotkey: "ctrl+win",
    assistHotkey: "win+alt",
    voiceAgentHotkey: "ctrl+shift",
    dictateHotkeyBehavior: "hold_to_talk",
    assistHotkeyBehavior: "hold_to_talk",
    voiceAgentHotkeyBehavior: "hold_to_talk",
    voiceAgentCloseBehavior: "continue",
    voiceAgentProfileId: "default",
    voiceAgentProfiles: [
      {
        id: "default",
        displayName: "Default Voice Agent",
        description: "Current Voice Mode behavior.",
      },
      {
        id: "brainstorming_companion",
        displayName: "Brainstorming Companion",
        description: "Critical, creative ideation partner.",
        voice: "Aoede",
      },
      {
        id: "humor_companion",
        displayName: "Humor Companion",
        description: "Playful conversation profile.",
        voice: "Puck",
      },
      {
        id: "support_companion",
        displayName: "Support Companion",
        description: "Warm, solution-oriented helper.",
        voice: "Charon",
      },
    ],
    voiceAgentRefinementPrompt: "",
    voiceAgentSessionSummary: true,
    autoStartOnLaunch: false,
    agentHotkey: "win+alt",
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
    assistOverlayMode: "small_feedback",
    voiceAgentOverlayMode: "small_feedback",
    overlayPosition: "top",
    overlayMovable: false,
    overlayFreeX: 0,
    overlayFreeY: 0,
    modelDownloadDir: "",
    vocabularyDictionary: "",
    saveAudio: true,
    audioRetentionDays: 7,
    dictateSilenceTimeoutSec: 3,
    selectedAudioDeviceId: "",
    hfHasUserToken: false,
    hfHasInstallToken: false,
    hfTokenSource: "none",
    activeProfiles: {},
    modelSelections: {
      dictate: {
        primaryProfileId: "stt.local.whispercpp",
        fallbackProfileId: "",
      },
      assist: {
        primaryProfileId: "assist.builtin.gemma4-e4b",
        fallbackProfileId: "",
      },
      voice_agent: {
        primaryProfileId: "realtime.google.gemini-native-audio",
        fallbackProfileId: "",
      },
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
    wakeword: {
      enabled: false,
      backend: "sherpa_kws",
      phraseId: "hey_quby",
      defaultMode: "voice_agent",
      threshold: 0,
      minConsecutiveFrames: 2,
      cooldownMs: 1500,
      debugMode: false,
      active: false,
      statusMessage: "",
      phraseCatalog: [],
      backendOptions: [],
    },
    homeAssistant: {
      url: "",
      tokenEnv: "",
      tokenConfigured: false,
      language: "",
    },
    piperTTS: {
      enabled: false,
      binary: "",
      voiceDir: "",
      timeoutSec: 0,
      defaultVoices: {},
      availableVoices: [],
    },
  } satisfies SpeechKitSettingsState,
  defaultWakewordSettings: {
    enabled: false,
    backend: "sherpa_kws",
    phraseId: "hey_quby",
    defaultMode: "voice_agent",
    threshold: 0,
    minConsecutiveFrames: 2,
    cooldownMs: 1500,
    debugMode: false,
    active: false,
    statusMessage: "",
    phraseCatalog: [],
    backendOptions: [],
  },
  fetchSettingsState: fetchSettingsStateMock,
  fetchModelProfiles: fetchModelProfilesMock,
  fetchOverlayState: fetchOverlayStateMock,
  resetOverlayPosition: resetOverlayPositionMock,
  saveSettingsState: saveSettingsStateMock,
  saveProviderCredential: saveProviderCredentialMock,
  clearProviderCredential: clearProviderCredentialMock,
  testProviderCredential: testProviderCredentialMock,
  updateProviderIntegration: updateProviderIntegrationMock,
  fetchAudioDevices: fetchAudioDevicesMock,
  setAudioDevice: setAudioDeviceMock,
  fetchAPIV1Dictionary: fetchAPIV1DictionaryMock,
  importAPIV1Dictionary: importAPIV1DictionaryMock,
  fetchDownloadCatalog: fetchDownloadCatalogMock,
  fetchDownloadJobs: fetchDownloadJobsMock,
  startModelDownload: startModelDownloadMock,
  cancelModelDownload: cancelModelDownloadMock,
  selectDownloadedModel: selectDownloadedModelMock,
  // ModeSource UI surface (Phase 5). The tests don't drive these
  // panels, so safe stubs that resolve to defaults are enough — the
  // components render a loading skeleton when the fetches haven't
  // returned yet.
  fetchAPIV1Modes: vi.fn(() =>
    Promise.resolve({
      contracts: [],
      settings: {
        dictation: {
          enabled: true,
          modeSource: "local",
          dictionaryEnabled: false,
        },
        assist: { enabled: true, modeSource: "local", ttsEnabled: true },
        voiceAgent: {
          enabled: true,
          modeSource: "local",
          sessionSummary: true,
          pipelineFallback: false,
        },
        serverConnection: {
          enabled: false,
          url: "",
          bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
          authMode: "bearer",
          bearerTokenSet: false,
          fallbackToLocal: true,
          requestTimeoutSec: 30,
        },
      },
    })
  ),
  patchAPIV1ModeSettings: vi.fn(() =>
    Promise.resolve({ enabled: true, modeSource: "local" })
  ),
  fetchAPIV1ServerConnection: vi.fn(() =>
    Promise.resolve({
      enabled: false,
      url: "",
      bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
      authMode: "bearer",
      bearerTokenSet: false,
      fallbackToLocal: true,
      requestTimeoutSec: 30,
    })
  ),
  patchAPIV1ServerConnection: vi.fn(() =>
    Promise.resolve({
      enabled: false,
      url: "",
      bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
      authMode: "bearer",
      bearerTokenSet: false,
      fallbackToLocal: true,
      requestTimeoutSec: 30,
    })
  ),
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

export const openStorageSettings = async () => {
  fireEvent.click(
    await screen.findByRole("button", { name: "Storage & Data" })
  );
};

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  fetchSettingsStateMock.mockReset();
  fetchModelProfilesMock.mockReset();
  saveSettingsStateMock.mockReset();
  fetchOverlayStateMock.mockReset();
  resetOverlayPositionMock.mockReset();
  updateProviderIntegrationMock.mockReset();
  saveProviderCredentialMock.mockReset();
  clearProviderCredentialMock.mockReset();
  testProviderCredentialMock.mockReset();
  fetchAudioDevicesMock.mockReset();
  setAudioDeviceMock.mockReset();
  fetchAPIV1DictionaryMock.mockReset();
  importAPIV1DictionaryMock.mockReset();
  fetchDownloadCatalogMock.mockReset();
  fetchDownloadJobsMock.mockReset();
  startModelDownloadMock.mockReset();
  cancelModelDownloadMock.mockReset();
  selectDownloadedModelMock.mockReset();
  openFileDialogMock.mockReset();

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
    assistOverlayMode: "small_feedback",
    voiceAgentOverlayMode: "small_feedback",
    hotkey: "ctrl+win",
    dictateHotkey: "ctrl+win",
    assistHotkey: "win+alt",
    voiceAgentHotkey: "ctrl+shift",
    dictateHotkeyBehavior: "hold_to_talk",
    assistHotkeyBehavior: "hold_to_talk",
    voiceAgentHotkeyBehavior: "toggle",
    modeEnabled: { dictate: true, assist: true, voice_agent: true },
    availableModes: { dictate: true, assist: true, voice_agent: true },
    agentHotkey: "win+alt",
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
  updateProviderIntegrationMock.mockResolvedValue({ message: "Saved" });
  setAudioDeviceMock.mockResolvedValue("Selected");
  fetchAPIV1DictionaryMock.mockResolvedValue({
    language: "en",
    entries: [],
  });
  importAPIV1DictionaryMock.mockImplementation(
    async (language: string, entries: unknown[]) => ({
      language,
      entries,
    })
  );
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
  openFileDialogMock.mockResolvedValue("");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

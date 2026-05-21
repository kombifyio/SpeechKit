export type OverlayMode = "pill" | "circle";
export type OverlayDesign = "default" | "kombify";
export type OverlayFeedbackMode = "big_productivity" | "small_feedback";
export type RuntimeMode = "none" | "dictate" | "assist" | "voice_agent";
export type AgentMode = "assist" | "voice_agent";
export type HotkeyBehavior = "hold_to_talk" | "toggle";
export type VoiceAgentCloseBehavior = "continue" | "new_chat";
export type VoiceAgentProfile = {
  id: string;
  displayName: string;
  description?: string;
  voice?: string;
  builtIn?: boolean;
};
export type StoreBackend = "sqlite" | "postgres";
export type Modality =
  | "stt"
  | "tts"
  | "realtime_voice"
  | "utility"
  | "assist"
  | "embedding"
  | "reranker";
export type ExecutionMode =
  | "local"
  | "self_hosted_http"
  | "hf_routed"
  | "hf_inference"
  | "openai_api"
  | "groq_api"
  | "google_api"
  | "ollama_local"
  | "openrouter_api";
export type ProviderKind =
  | "local_built_in"
  | "local_provider"
  | "cloud_provider"
  | "direct_provider";
export type ModelCapability =
  | "transcription"
  | "stt"
  | "audio_input"
  | "llm"
  | "tts"
  | "realtime_audio"
  | "pipeline_fallback"
  | "tool_calling"
  | "dictionary_prompt"
  | "dictionary_native_hints"
  | "session_summary";
export type IntelligenceKind = "user" | "utility" | "brainstorming";
export type LogType = "info" | "warn" | "error" | "success";
export type AvailableModes = Record<
  "dictate" | "assist" | "voice_agent",
  boolean
>;
export type ModeEnabled = Record<"dictate" | "assist" | "voice_agent", boolean>;

export type AudioDevice = {
  deviceId: string;
  label: string;
  groupId?: string;
  isDefault?: boolean;
};

export type AudioAsset = {
  storageKind: string;
  mimeType?: string;
  sizeBytes: number;
  durationMs: number;
};

export type ModelProfile = {
  id: string;
  modality: Modality;
  name: string;
  providerKind?: ProviderKind;
  executionMode?: ExecutionMode;
  source?: string;
  provider?: string;
  description?: string;
  capabilities?: ModelCapability[];
  adapterKind?: string;
  variants?: ModelVariant[];
  recommended?: boolean;
  experimental?: boolean;
};

export type ModelVariant = {
  id: string;
  name: string;
  modelId: string;
  description?: string;
  recommended?: boolean;
};

export type SpeechKitOverlayState = {
  state: "idle" | "recording" | "processing" | "done";
  phase: "idle" | "listening" | "speaking" | "thinking" | "done";
  text: string;
  level: number;
  visible: boolean;
  visualizer: OverlayMode;
  design: OverlayDesign;
  assistOverlayMode: OverlayFeedbackMode;
  voiceAgentOverlayMode: OverlayFeedbackMode;
  hotkey: string;
  dictateHotkey: string;
  assistHotkey: string;
  voiceAgentHotkey: string;
  dictateHotkeyBehavior: HotkeyBehavior;
  assistHotkeyBehavior: HotkeyBehavior;
  voiceAgentHotkeyBehavior: HotkeyBehavior;
  modeEnabled: ModeEnabled;
  agentHotkey: string;
  activeMode: RuntimeMode;
  availableModes: AvailableModes;
  position: "top" | "bottom" | "left" | "right";
  movable: boolean;
  positionFreeX: number;
  positionFreeY: number;
  lastTranscription: string;
  quickNoteMode: boolean;
  selectedAudioDeviceId: string;
  selectedOutputDeviceId?: string;
  activeProfiles: Partial<Record<Modality, string>>;
};

export type ProviderCredentialState = {
  provider: string;
  label: string;
  envName: string;
  available: boolean;
  hasStoredSecret: boolean;
  source: "none" | "user" | "install" | "env";
};

export type ProviderIntegrationKind =
  | "cloud_gateway"
  | "direct_api"
  | "local_provider";

export type ProviderIntegrationState = {
  provider: string;
  label: string;
  enabled: boolean;
  providerKind: ProviderKind;
  integrationKind: ProviderIntegrationKind;
  credentialRequired: boolean;
  available: boolean;
  hasStoredSecret: boolean;
  source: "none" | "user" | "install" | "env";
  envName?: string;
  setupUrl?: string;
  supportedModes: Array<"dictate" | "assist" | "voice_agent">;
};

export type ModeModelSelectionState = {
  primaryProfileId: string;
  fallbackProfileId: string;
};

export type ModelSelectionsState = Record<
  "dictate" | "assist" | "voice_agent",
  ModeModelSelectionState
>;

export type ModeContract = {
  mode: "dictation" | "assist" | "voice_agent";
  intelligence: IntelligenceKind;
  input: string;
  output: string;
  allowed: ModelCapability[];
  forbidden: ModelCapability[];
};

export type APIV1ModeSettings = {
  dictation: ModeModelSetting & {
    dictionaryEnabled: boolean;
  };
  assist: ModeModelSetting & {
    ttsEnabled: boolean;
    utilityRegistry?: string;
  };
  voiceAgent: ModeModelSetting & {
    sessionSummary: boolean;
    pipelineFallback: boolean;
    closeBehavior?: VoiceAgentCloseBehavior;
    agentProfileId?: string;
  };
  serverConnection: ServerConnectionSetting;
};

export type ModeSource = "local" | "server";

export type ModeModelSetting = {
  enabled: boolean;
  hotkey?: string;
  hotkeyBehavior?: HotkeyBehavior;
  primaryProfileId?: string;
  fallbackProfileId?: string;
  /**
   * "local" runs the mode against the in-process Framework kernel
   * (default, pre-0.26 behaviour). "server" routes the mode through
   * the configured ServerConnection. Empty/missing is treated as
   * "local" by the backend.
   */
  modeSource?: ModeSource;
};

export type ServerConnectionSetting = {
  enabled: boolean;
  activeTargetId?: string;
  url: string;
  bearerTokenEnv?: string;
  authMode?: "bearer" | "api_key";
  /** True iff the env var named by bearerTokenEnv is set in the host process. */
  bearerTokenSet: boolean;
  fallbackToLocal: boolean;
  requestTimeoutSec: number;
  targets?: ServerConnectionTarget[];
};

export type ServerConnectionTarget = {
  id: string;
  label: string;
  url: string;
  authMode: "bearer" | "api_key";
  bearerTokenEnv?: string;
  bearerTokenSet: boolean;
  fallbackToLocal: boolean;
  requestTimeoutSec: number;
};

export type ServerConnectionSmokeRequest = {
  url: string;
  bearerTokenEnv?: string;
  authMode?: "bearer" | "api_key";
  token?: string;
  requestTimeoutSec?: number;
};

export type ServerConnectionSmokeResponse = {
  ok: boolean;
  status: "ok" | "failed" | string;
  message: string;
  healthStatus?: number;
  readyStatus?: number;
};

export type APIV1ModesResponse = {
  contracts: ModeContract[];
  settings: APIV1ModeSettings;
};

export type ReadinessRequirement = {
  id: string;
  label: string;
  category: "credential" | "runtime" | "capability" | "model" | string;
  required: boolean;
  ready: boolean;
  missing?: string;
};

export type ReadinessAction = {
  id: string;
  label: string;
  kind:
    | "configure_credential"
    | "configure_provider"
    | "configure_runtime"
    | "download_artifact"
    | "select_artifact"
    | "install_runtime"
    | string;
  target?: string;
};

export type ReadinessArtifact = {
  id: string;
  name: string;
  kind: DownloadKind | string;
  sizeLabel?: string;
  sizeBytes?: number;
  available: boolean;
  selected: boolean;
  runtimeReady?: boolean;
  runtimeProblem?: string;
  recommended?: boolean;
};

export type ProviderReadiness = {
  schemaVersion?: "provider-readiness.v1" | string;
  profileId: string;
  mode: "dictation" | "assist" | "voice_agent";
  providerKind: ProviderKind;
  executionMode?: ExecutionMode;
  modelId?: string;
  source?: string;
  active: boolean;
  default: boolean;
  configured: boolean;
  credentialsReady: boolean;
  runtimeReady: boolean;
  capabilityReady: boolean;
  ready: boolean;
  missing?: string[];
  requirements?: ReadinessRequirement[];
  actions?: ReadinessAction[];
  artifacts?: ReadinessArtifact[];
};

export type APIV1ProviderArtifactsResponse = {
  artifacts: DownloadItem[];
  jobs: DownloadJob[];
};

export type DictionaryEntry = {
  id?: number;
  spoken: string;
  canonical: string;
  language?: string;
  source?: string;
  enabled: boolean;
  usageCount: number;
};

export type APIV1DictionaryResponse = {
  language: string;
  entries: DictionaryEntry[];
};

export type VoiceAgentTurnRecord = {
  role: "user" | "assistant";
  text: string;
  createdAt?: string;
};

export type VoiceAgentSessionSummary = {
  title?: string;
  summary: string;
  ideas?: string[];
  decisions?: string[];
  openQuestions?: string[];
  nextSteps?: string[];
  rawText?: string;
};

export type VoiceAgentSessionRecord = {
  id: number;
  startedAt: string;
  endedAt: string;
  language: string;
  providerProfileId?: string;
  runtimeKind?: "native_realtime" | "pipeline_fallback" | string;
  transcript?: string;
  turns?: VoiceAgentTurnRecord[];
  summary: VoiceAgentSessionSummary;
  createdAt: string;
};

export type APIV1ProfilesResponse = {
  profiles: ModelProfile[];
  activeProfiles: Partial<Record<Modality, string>>;
  groups: Record<string, string[]>;
  contracts: ModeContract[];
};

export type SpeechKitSettingsState = {
  overlayEnabled: boolean;
  storeBackend: StoreBackend;
  sqlitePath: string;
  postgresConfigured: boolean;
  postgresDSN: string;
  maxAudioStorageMB: number;
  modelDownloadDir: string;
  hfAvailable: boolean;
  hfEnabled: boolean;
  hfHasUserToken: boolean;
  hfHasInstallToken: boolean;
  hfTokenSource: "none" | "user" | "install" | "env";
  hotkey: string;
  dictateHotkey: string;
  assistHotkey: string;
  voiceAgentHotkey: string;
  dictateHotkeyBehavior: HotkeyBehavior;
  assistHotkeyBehavior: HotkeyBehavior;
  voiceAgentHotkeyBehavior: HotkeyBehavior;
  voiceAgentCloseBehavior: VoiceAgentCloseBehavior;
  voiceAgentProfileId: string;
  voiceAgentProfiles: VoiceAgentProfile[];
  voiceAgentRefinementPrompt: string;
  voiceAgentSessionSummary: boolean;
  autoStartOnLaunch: boolean;
  modeEnabled: ModeEnabled;
  agentHotkey: string;
  agentMode: AgentMode;
  activeMode: RuntimeMode;
  availableModes: AvailableModes;
  hfModel: "openai/whisper-large-v3-turbo" | "openai/whisper-large-v3";
  visualizer: OverlayMode;
  design: OverlayDesign;
  assistOverlayMode: OverlayFeedbackMode;
  voiceAgentOverlayMode: OverlayFeedbackMode;
  overlayPosition: "top" | "bottom" | "left" | "right";
  overlayMovable: boolean;
  overlayFreeX: number;
  overlayFreeY: number;
  vocabularyDictionary: string;
  saveAudio: boolean;
  audioRetentionDays: number;
  selectedAudioDeviceId: string;
  selectedOutputDeviceId?: string;
  profiles?: ModelProfile[];
  activeProfiles: Partial<Record<Modality, string>>;
  modelSelections: ModelSelectionsState;
  providerCredentials?: Record<string, ProviderCredentialState>;
  providerIntegrations?: Record<string, ProviderIntegrationState>;
  wakeword: WakewordSettings;
};

export type WakewordDefaultMode = "dictate" | "assist" | "voice_agent";
export type WakewordBackend =
  | "sherpa_kws"
  | "livekit_openwakeword"
  | "stt_phrase";

export type WakewordBackendOption = {
  id: WakewordBackend;
  label: string;
  description: string;
  status: string;
  available: boolean;
  implemented: boolean;
  recommended?: boolean;
};

export type WakewordPhraseCatalogEntry = {
  id: string;
  displayName: string;
  variants: string[];
  fileName: string;
  trainingTemplate: string;
  recommendedThreshold: number;
  notes: string;
};

export type WakewordSettings = {
  enabled: boolean;
  backend: WakewordBackend;
  phraseId: string;
  defaultMode: WakewordDefaultMode;
  threshold: number;
  minConsecutiveFrames: number;
  cooldownMs: number;
  active: boolean;
  statusMessage: string;
  phraseCatalog: WakewordPhraseCatalogEntry[];
  backendOptions: WakewordBackendOption[];
};

export type AudioDevicesResponse = {
  devices: AudioDevice[];
  selectedDeviceId: string;
};

export type AudioDeviceUpdateResponse = {
  message?: string;
  selectedDeviceId?: string;
};

export type TranscriptionRecord = {
  id: number;
  text: string;
  language: string;
  provider: string;
  model?: string;
  durationMs?: number;
  latencyMs: number;
  audio?: AudioAsset;
  createdAt: string;
};

export type QuickNote = {
  id: number;
  text: string;
  language: string;
  provider: string;
  durationMs?: number;
  latencyMs: number;
  audio?: AudioAsset;
  pinned: boolean;
  createdAt: string;
  updatedAt: string;
};

export type DashboardStats = {
  transcriptions: number;
  quickNotes: number;
  totalWords: number;
  totalAudioDurationMs: number;
  averageWordsPerMinute: number;
  averageLatencyMs: number;
};

export type LogEntry = {
  message: string;
  type: LogType;
  timestamp: string;
};

export type DownloadKind = "http" | "ollama";
export type DownloadStatus =
  | "pending"
  | "running"
  | "done"
  | "failed"
  | "cancelled";

export type DownloadItem = {
  id: string;
  profileId: string;
  name: string;
  description: string;
  sizeLabel: string;
  sizeBytes: number;
  kind: DownloadKind;
  url?: string;
  ollamaModel?: string;
  license: string;
  available: boolean;
  selected: boolean;
  runtimeReady?: boolean;
  runtimeProblem?: string;
  recommended?: boolean;
};

export type DownloadJob = {
  id: string;
  modelId: string;
  profileId: string;
  status: DownloadStatus;
  progress: number;
  bytesDone: number;
  totalBytes: number;
  statusText: string;
  error?: string;
};

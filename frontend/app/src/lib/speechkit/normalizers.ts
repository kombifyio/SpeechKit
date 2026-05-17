import {
  builtInPrimaryModelSelections,
  defaultOverlayState,
  defaultSettingsState,
  defaultVoiceAgentProfiles,
} from "./defaults";
import type {
  AgentMode,
  AvailableModes,
  HotkeyBehavior,
  ModeModelSelectionState,
  Modality,
  ModelSelectionsState,
  OverlayFeedbackMode,
  RuntimeMode,
  SpeechKitOverlayState,
  SpeechKitSettingsState,
  VoiceAgentProfile,
  WakewordDefaultMode,
  WakewordPhraseCatalogEntry,
  WakewordSettings,
} from "./types";

function readStringField(
  payload: Record<string, unknown> | null | undefined,
  key: string,
): string | undefined {
  if (!payload || !(key in payload)) {
    return undefined;
  }
  const value = payload[key];
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function normalizeHotkeyBehavior(
  payload: Record<string, unknown> | null | undefined,
  key: string,
  fallback: HotkeyBehavior,
): HotkeyBehavior {
  const value = readStringField(payload, key);
  if (value === "toggle") {
    return "toggle";
  }
  if (value === "push_to_talk") {
    return "push_to_talk";
  }
  return fallback;
}

function normalizeOverlayFeedbackMode(
  payload: Record<string, unknown> | null | undefined,
  key: string,
  fallback: OverlayFeedbackMode,
): OverlayFeedbackMode {
  const value = readStringField(payload, key);
  if (value === "big_productivity") {
    return "big_productivity";
  }
  if (value === "small_feedback") {
    return "small_feedback";
  }
  return fallback;
}

function normalizeModeFlags(
  payload: Record<string, unknown> | null | undefined,
  key: "modeEnabled" | "availableModes",
  fallback: AvailableModes,
): AvailableModes {
  const raw = payload?.[key];
  if (!raw || typeof raw !== "object") {
    return fallback;
  }

  const map = raw as Partial<Record<keyof AvailableModes, unknown>>;
  return {
    dictate: typeof map.dictate === "boolean" ? map.dictate : fallback.dictate,
    assist: typeof map.assist === "boolean" ? map.assist : fallback.assist,
    voice_agent:
      typeof map.voice_agent === "boolean"
        ? map.voice_agent
        : fallback.voice_agent,
  };
}

function normalizeAvailableModes(
  payload: Record<string, unknown> | null | undefined,
  modeEnabled: AvailableModes,
  hotkeys: AvailableModes,
): AvailableModes {
  const derived = {
    dictate: modeEnabled.dictate && hotkeys.dictate,
    assist: modeEnabled.assist && hotkeys.assist,
    voice_agent: modeEnabled.voice_agent && hotkeys.voice_agent,
  };
  return normalizeModeFlags(payload, "availableModes", derived);
}

function resolveAssistHotkey(
  payload: Record<string, unknown> | null | undefined,
  fallback: string,
): string {
  if (!payload) {
    return fallback;
  }

  const explicit = readStringField(payload, "assistHotkey");
  if (explicit !== undefined) {
    return explicit;
  }

  const legacy = readStringField(payload, "agentHotkey");
  const legacyMode = readStringField(payload, "agentMode");
  if (legacy !== undefined && legacyMode !== "voice_agent") {
    return legacy;
  }

  return "";
}

function resolveVoiceAgentHotkey(
  payload: Record<string, unknown> | null | undefined,
  fallback: string,
): string {
  if (!payload) {
    return fallback;
  }

  const explicit = readStringField(payload, "voiceAgentHotkey");
  if (explicit !== undefined) {
    return explicit;
  }

  const legacy = readStringField(payload, "agentHotkey");
  const legacyMode = readStringField(payload, "agentMode");
  if (legacy !== undefined && legacyMode === "voice_agent") {
    return legacy;
  }

  return "";
}

function normalizeRuntimeMode(
  rawMode: string | undefined,
  availableModes: AvailableModes,
  agentMode: AgentMode = "assist",
): RuntimeMode {
  let mode: RuntimeMode;
  switch (rawMode) {
    case "dictate":
    case "assist":
    case "voice_agent":
    case "none":
      mode = rawMode;
      break;
    case "agent":
      mode = agentMode === "voice_agent" ? "voice_agent" : "assist";
      break;
    default:
      mode = "none";
      break;
  }

  if (mode === "dictate" && !availableModes.dictate) {
    return "none";
  }
  if (mode === "assist" && !availableModes.assist) {
    return "none";
  }
  if (mode === "voice_agent" && !availableModes.voice_agent) {
    return "none";
  }
  return mode;
}

export function deriveLegacyAgentMode(
  assistHotkey: string,
  voiceAgentHotkey: string,
  activeMode: RuntimeMode,
  fallback: AgentMode = "assist",
): AgentMode {
  if (activeMode === "voice_agent" && voiceAgentHotkey) {
    return "voice_agent";
  }
  if (activeMode === "assist" && assistHotkey) {
    return "assist";
  }
  if (assistHotkey) {
    return "assist";
  }
  if (voiceAgentHotkey) {
    return "voice_agent";
  }
  return fallback === "voice_agent" ? "voice_agent" : "assist";
}

export function deriveLegacyAgentHotkey(
  assistHotkey: string,
  voiceAgentHotkey: string,
  activeMode: RuntimeMode,
): string {
  if (activeMode === "voice_agent" && voiceAgentHotkey) {
    return voiceAgentHotkey;
  }
  if (activeMode === "assist" && assistHotkey) {
    return assistHotkey;
  }
  return assistHotkey || voiceAgentHotkey;
}

function cloneVoiceAgentProfiles(
  profiles: VoiceAgentProfile[],
): VoiceAgentProfile[] {
  return profiles.map((profile) => ({ ...profile }));
}

function normalizeVoiceAgentProfileRecord(
  value: unknown,
): VoiceAgentProfile | null {
  if (!value || typeof value !== "object") {
    return null;
  }

  const record = value as Record<string, unknown>;
  const id = typeof record.id === "string" ? record.id.trim() : "";
  if (!id) {
    return null;
  }

  const rawDisplayName = record.displayName ?? record.display_name;
  const displayName =
    typeof rawDisplayName === "string" && rawDisplayName.trim()
      ? rawDisplayName.trim()
      : id;
  const profile: VoiceAgentProfile = { id, displayName };
  if (typeof record.description === "string" && record.description.trim()) {
    profile.description = record.description.trim();
  }
  if (typeof record.voice === "string" && record.voice.trim()) {
    profile.voice = record.voice.trim();
  }
  const rawBuiltIn = record.builtIn ?? record.built_in ?? record.builtin;
  if (typeof rawBuiltIn === "boolean") {
    profile.builtIn = rawBuiltIn;
  }
  return profile;
}

function normalizeVoiceAgentProfiles(
  payload: Record<string, unknown> | null | undefined,
  fallback: VoiceAgentProfile[],
): VoiceAgentProfile[] {
  const fallbackProfiles = fallback.length
    ? fallback
    : defaultVoiceAgentProfiles;
  const raw = payload?.voiceAgentProfiles;
  if (!Array.isArray(raw)) {
    return cloneVoiceAgentProfiles(fallbackProfiles);
  }

  const profiles = raw
    .map((profile) => normalizeVoiceAgentProfileRecord(profile))
    .filter((profile): profile is VoiceAgentProfile => profile !== null);
  return profiles.length ? profiles : cloneVoiceAgentProfiles(fallbackProfiles);
}

function normalizeVoiceAgentProfileId(
  value: string | undefined,
  profiles: VoiceAgentProfile[],
  fallback: string,
): string {
  const candidate = value?.trim() ?? "";
  if (candidate && profiles.some((profile) => profile.id === candidate)) {
    return candidate;
  }
  if (fallback && profiles.some((profile) => profile.id === fallback)) {
    return fallback;
  }
  if (profiles.some((profile) => profile.id === "default")) {
    return "default";
  }
  return profiles[0]?.id ?? "default";
}

export function normalizeOverlayState(
  payload: Partial<SpeechKitOverlayState> | null | undefined,
): SpeechKitOverlayState {
  const base = { ...defaultOverlayState };
  const record = (payload ?? null) as Record<string, unknown> | null;
  const hotkey =
    readStringField(record, "hotkey") ??
    readStringField(record, "dictateHotkey") ??
    base.hotkey;
  const dictateHotkey =
    readStringField(record, "dictateHotkey") ??
    readStringField(record, "hotkey") ??
    base.dictateHotkey;
  const assistHotkey = resolveAssistHotkey(record, base.assistHotkey);
  const voiceAgentHotkey = resolveVoiceAgentHotkey(
    record,
    base.voiceAgentHotkey,
  );
  const dictateHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "dictateHotkeyBehavior",
    base.dictateHotkeyBehavior,
  );
  const assistHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "assistHotkeyBehavior",
    base.assistHotkeyBehavior,
  );
  const voiceAgentHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "voiceAgentHotkeyBehavior",
    base.voiceAgentHotkeyBehavior,
  );
  const hotkeyModes = {
    dictate: dictateHotkey !== "",
    assist: assistHotkey !== "",
    voice_agent: voiceAgentHotkey !== "",
  };
  const modeEnabled = normalizeModeFlags(record, "modeEnabled", hotkeyModes);
  const availableModes = normalizeAvailableModes(
    record,
    modeEnabled,
    hotkeyModes,
  );
  const activeMode = normalizeRuntimeMode(
    readStringField(record, "activeMode"),
    availableModes,
  );
  const agentHotkey =
    readStringField(record, "agentHotkey") ??
    deriveLegacyAgentHotkey(assistHotkey, voiceAgentHotkey, activeMode);
  const assistOverlayMode = normalizeOverlayFeedbackMode(
    record,
    "assistOverlayMode",
    base.assistOverlayMode,
  );
  const voiceAgentOverlayMode = normalizeOverlayFeedbackMode(
    record,
    "voiceAgentOverlayMode",
    base.voiceAgentOverlayMode,
  );

  return {
    ...base,
    ...(payload ?? {}),
    hotkey,
    dictateHotkey,
    assistHotkey,
    voiceAgentHotkey,
    dictateHotkeyBehavior,
    assistHotkeyBehavior,
    voiceAgentHotkeyBehavior,
    modeEnabled,
    agentHotkey,
    activeMode,
    availableModes,
    assistOverlayMode,
    voiceAgentOverlayMode,
    selectedAudioDeviceId:
      payload?.selectedAudioDeviceId ??
      (payload as { audioDeviceId?: string } | undefined)?.audioDeviceId ??
      base.selectedAudioDeviceId,
    selectedOutputDeviceId:
      payload?.selectedOutputDeviceId ??
      (payload as { audioOutputDeviceId?: string } | undefined)
        ?.audioOutputDeviceId ??
      base.selectedOutputDeviceId,
    activeProfiles: payload?.activeProfiles ?? base.activeProfiles,
  };
}

export function normalizeSettingsState(
  payload: Partial<SpeechKitSettingsState> | null | undefined,
): SpeechKitSettingsState {
  const base = { ...defaultSettingsState };
  const record = (payload ?? null) as Record<string, unknown> | null;
  const hotkey =
    readStringField(record, "hotkey") ??
    readStringField(record, "dictateHotkey") ??
    base.hotkey;
  const dictateHotkey =
    readStringField(record, "dictateHotkey") ??
    readStringField(record, "hotkey") ??
    base.dictateHotkey;
  const assistHotkey = resolveAssistHotkey(record, base.assistHotkey);
  const voiceAgentHotkey = resolveVoiceAgentHotkey(
    record,
    base.voiceAgentHotkey,
  );
  const dictateHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "dictateHotkeyBehavior",
    base.dictateHotkeyBehavior,
  );
  const assistHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "assistHotkeyBehavior",
    base.assistHotkeyBehavior,
  );
  const voiceAgentHotkeyBehavior = normalizeHotkeyBehavior(
    record,
    "voiceAgentHotkeyBehavior",
    base.voiceAgentHotkeyBehavior,
  );
  const voiceAgentCloseBehavior =
    readStringField(record, "voiceAgentCloseBehavior") === "new_chat"
      ? "new_chat"
      : "continue";
  const voiceAgentProfiles = normalizeVoiceAgentProfiles(
    record,
    base.voiceAgentProfiles,
  );
  const voiceAgentProfileId = normalizeVoiceAgentProfileId(
    readStringField(record, "voiceAgentProfileId"),
    voiceAgentProfiles,
    base.voiceAgentProfileId,
  );
  const voiceAgentRefinementPrompt =
    readStringField(record, "voiceAgentRefinementPrompt") ?? "";
  const voiceAgentSessionSummary =
    typeof record?.voiceAgentSessionSummary === "boolean"
      ? record.voiceAgentSessionSummary
      : base.voiceAgentSessionSummary;
  const autoStartOnLaunch =
    typeof record?.autoStartOnLaunch === "boolean"
      ? record.autoStartOnLaunch
      : typeof record?.voiceAgentAutoStart === "boolean"
        ? record.voiceAgentAutoStart
        : base.autoStartOnLaunch;
  const hotkeyModes = {
    dictate: dictateHotkey !== "",
    assist: assistHotkey !== "",
    voice_agent: voiceAgentHotkey !== "",
  };
  const modeEnabled = normalizeModeFlags(record, "modeEnabled", hotkeyModes);
  const availableModes = normalizeAvailableModes(
    record,
    modeEnabled,
    hotkeyModes,
  );
  const agentMode =
    readStringField(record, "agentMode") === "voice_agent"
      ? "voice_agent"
      : deriveLegacyAgentMode(
          assistHotkey,
          voiceAgentHotkey,
          "none",
          base.agentMode,
        );
  const activeMode = normalizeRuntimeMode(
    readStringField(record, "activeMode"),
    availableModes,
    agentMode,
  );
  const agentHotkey =
    readStringField(record, "agentHotkey") ??
    deriveLegacyAgentHotkey(assistHotkey, voiceAgentHotkey, activeMode);
  const storeBackend =
    payload?.storeBackend === "postgres" ? "postgres" : base.storeBackend;
  const activeProfiles = payload?.activeProfiles ?? base.activeProfiles;
  const modelSelections = normalizeModelSelections(record, activeProfiles);
  const assistOverlayMode = normalizeOverlayFeedbackMode(
    record,
    "assistOverlayMode",
    base.assistOverlayMode,
  );
  const voiceAgentOverlayMode = normalizeOverlayFeedbackMode(
    record,
    "voiceAgentOverlayMode",
    base.voiceAgentOverlayMode,
  );

  const sanitizedPayload = {
    ...((payload ?? {}) as Record<string, unknown>),
  };
  delete sanitizedPayload.voiceAgentFrameworkPrompt;
  delete sanitizedPayload.voiceAgentInstruction;

  return {
    ...base,
    ...sanitizedPayload,
    storeBackend,
    sqlitePath: payload?.sqlitePath ?? base.sqlitePath,
    postgresConfigured: payload?.postgresConfigured ?? base.postgresConfigured,
    postgresDSN: payload?.postgresDSN ?? base.postgresDSN,
    maxAudioStorageMB: payload?.maxAudioStorageMB ?? base.maxAudioStorageMB,
    modelDownloadDir: payload?.modelDownloadDir ?? base.modelDownloadDir,
    hotkey,
    dictateHotkey,
    assistHotkey,
    voiceAgentHotkey,
    dictateHotkeyBehavior,
    assistHotkeyBehavior,
    voiceAgentHotkeyBehavior,
    voiceAgentCloseBehavior,
    voiceAgentProfileId,
    voiceAgentProfiles,
    voiceAgentRefinementPrompt,
    voiceAgentSessionSummary,
    autoStartOnLaunch,
    modeEnabled,
    agentHotkey,
    agentMode,
    activeMode,
    availableModes,
    assistOverlayMode,
    voiceAgentOverlayMode,
    selectedAudioDeviceId:
      payload?.selectedAudioDeviceId ??
      (payload as { audioDeviceId?: string } | undefined)?.audioDeviceId ??
      base.selectedAudioDeviceId,
    selectedOutputDeviceId:
      payload?.selectedOutputDeviceId ??
      (payload as { audioOutputDeviceId?: string } | undefined)
        ?.audioOutputDeviceId ??
      base.selectedOutputDeviceId,
    vocabularyDictionary:
      payload?.vocabularyDictionary ?? base.vocabularyDictionary,
    profiles: payload?.profiles ?? base.profiles,
    activeProfiles,
    modelSelections,
    providerCredentials:
      payload?.providerCredentials ?? base.providerCredentials,
    providerIntegrations:
      payload?.providerIntegrations ?? base.providerIntegrations,
    wakeword: normalizeWakewordSettings(record, base.wakeword),
  };
}

function normalizeWakewordDefaultMode(
  value: unknown,
  fallback: WakewordDefaultMode,
): WakewordDefaultMode {
  if (typeof value !== "string") return fallback;
  const v = value.trim().toLowerCase();
  if (v === "dictate" || v === "assist" || v === "voice_agent") return v;
  return fallback;
}

function normalizeWakewordPhraseCatalog(
  raw: unknown,
): WakewordPhraseCatalogEntry[] {
  if (!Array.isArray(raw)) return [];
  const out: WakewordPhraseCatalogEntry[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const obj = entry as Record<string, unknown>;
    const id = typeof obj.id === "string" ? obj.id.trim() : "";
    if (!id) continue;
    const variantsRaw = Array.isArray(obj.variants) ? obj.variants : [];
    const variants = variantsRaw
      .filter((v): v is string => typeof v === "string")
      .map((v) => v.trim())
      .filter((v) => v.length > 0);
    out.push({
      id,
      displayName: typeof obj.displayName === "string" ? obj.displayName : id,
      variants,
      fileName: typeof obj.fileName === "string" ? obj.fileName : "",
      trainingTemplate:
        typeof obj.trainingTemplate === "string" ? obj.trainingTemplate : "",
      recommendedThreshold:
        typeof obj.recommendedThreshold === "number"
          ? obj.recommendedThreshold
          : 0,
      notes: typeof obj.notes === "string" ? obj.notes : "",
    });
  }
  return out;
}

function normalizeWakewordSettings(
  record: Record<string, unknown> | null,
  base: WakewordSettings,
): WakewordSettings {
  const raw = record?.wakeword;
  if (!raw || typeof raw !== "object") return base;
  const obj = raw as Record<string, unknown>;
  return {
    enabled: typeof obj.enabled === "boolean" ? obj.enabled : base.enabled,
    phraseId:
      typeof obj.phraseId === "string" ? obj.phraseId.trim() : base.phraseId,
    defaultMode: normalizeWakewordDefaultMode(obj.defaultMode, base.defaultMode),
    threshold:
      typeof obj.threshold === "number" && obj.threshold >= 0
        ? obj.threshold
        : base.threshold,
    minConsecutiveFrames:
      typeof obj.minConsecutiveFrames === "number" &&
      obj.minConsecutiveFrames >= 0
        ? Math.round(obj.minConsecutiveFrames)
        : base.minConsecutiveFrames,
    cooldownMs:
      typeof obj.cooldownMs === "number" && obj.cooldownMs >= 0
        ? Math.round(obj.cooldownMs)
        : base.cooldownMs,
    active: typeof obj.active === "boolean" ? obj.active : false,
    statusMessage:
      typeof obj.statusMessage === "string" ? obj.statusMessage : "",
    phraseCatalog: normalizeWakewordPhraseCatalog(obj.phraseCatalog),
  };
}

function normalizeModeSelectionEntry(
  value: unknown,
  fallbackPrimary = "",
): ModeModelSelectionState {
  if (!value || typeof value !== "object") {
    return { primaryProfileId: fallbackPrimary, fallbackProfileId: "" };
  }

  const entry = value as {
    primaryProfileId?: unknown;
    fallbackProfileId?: unknown;
  };
  const rawPrimaryProfileId =
    typeof entry.primaryProfileId === "string"
      ? entry.primaryProfileId.trim()
      : "";
  const primaryProfileId =
    rawPrimaryProfileId !== "" ? rawPrimaryProfileId : fallbackPrimary;
  const fallbackProfileId =
    typeof entry.fallbackProfileId === "string"
      ? entry.fallbackProfileId.trim()
      : "";

  if (primaryProfileId !== "" && primaryProfileId === fallbackProfileId) {
    return { primaryProfileId, fallbackProfileId: "" };
  }

  return { primaryProfileId, fallbackProfileId };
}

function normalizeModelSelections(
  payload: Record<string, unknown> | null | undefined,
  activeProfiles: Partial<Record<Modality, string>>,
): ModelSelectionsState {
  const raw = payload?.modelSelections;
  const selections =
    raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};

  return {
    dictate: normalizeModeSelectionEntry(
      selections.dictate,
      activeProfiles.stt ??
        builtInPrimaryModelSelections.dictate.primaryProfileId,
    ),
    assist: normalizeModeSelectionEntry(
      selections.assist,
      activeProfiles.assist ??
        builtInPrimaryModelSelections.assist.primaryProfileId,
    ),
    voice_agent: normalizeModeSelectionEntry(
      selections.voice_agent,
      activeProfiles.realtime_voice ??
        builtInPrimaryModelSelections.voice_agent.primaryProfileId,
    ),
  };
}

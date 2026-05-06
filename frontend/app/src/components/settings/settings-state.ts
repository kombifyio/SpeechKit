import type { SpeechKitSettingsState } from "@/lib/speechkit";

export const MODE_DEFAULT_BASES = {
  dictate: "ctrl+win",
  assist: "win+alt",
  voice_agent: "ctrl+shift",
} as const;

export const MODE_HOTKEY_FIELDS = {
  dictate: "dictateHotkey",
  assist: "assistHotkey",
  voice_agent: "voiceAgentHotkey",
} as const;

export const MODE_HOTKEY_BEHAVIOR_FIELDS = {
  dictate: "dictateHotkeyBehavior",
  assist: "assistHotkeyBehavior",
  voice_agent: "voiceAgentHotkeyBehavior",
} as const;

export const MODE_SELECTION_KEYS = {
  stt: "dictate",
  assist: "assist",
  realtime_voice: "voice_agent",
} as const;

export const DEFAULT_SQLITE_FILENAME = "feedback.db";

export type ConfigurableMode = keyof typeof MODE_HOTKEY_FIELDS;

export function deriveAvailableModes(settings: SpeechKitSettingsState) {
  return {
    dictate:
      settings.modeEnabled.dictate && settings.dictateHotkey.trim().length > 0,
    assist:
      settings.modeEnabled.assist && settings.assistHotkey.trim().length > 0,
    voice_agent:
      settings.modeEnabled.voice_agent &&
      settings.voiceAgentHotkey.trim().length > 0,
  };
}

export function reconcileSettingsState(
  settings: SpeechKitSettingsState,
): SpeechKitSettingsState {
  const modeEnabled = {
    dictate:
      settings.modeEnabled.dictate && settings.dictateHotkey.trim().length > 0,
    assist:
      settings.modeEnabled.assist && settings.assistHotkey.trim().length > 0,
    voice_agent:
      settings.modeEnabled.voice_agent &&
      settings.voiceAgentHotkey.trim().length > 0,
  };
  const availableModes = deriveAvailableModes({
    ...settings,
    modeEnabled,
  });

  return {
    ...settings,
    hotkey: settings.dictateHotkey,
    modeEnabled,
    availableModes,
    activeMode: availableModes[settings.activeMode as ConfigurableMode]
      ? settings.activeMode
      : "none",
  };
}

export function directoryFromPath(path: string) {
  const trimmedPath = path.trim();
  const lastSeparator = Math.max(
    trimmedPath.lastIndexOf("/"),
    trimmedPath.lastIndexOf("\\"),
  );
  if (lastSeparator <= 0) return "";
  return trimmedPath.slice(0, lastSeparator);
}

export function sqliteFilenameFromPath(path: string) {
  const trimmedPath = path.trim();
  const lastSeparator = Math.max(
    trimmedPath.lastIndexOf("/"),
    trimmedPath.lastIndexOf("\\"),
  );
  const filename =
    lastSeparator >= 0 ? trimmedPath.slice(lastSeparator + 1) : trimmedPath;
  return filename.includes(".") ? filename : DEFAULT_SQLITE_FILENAME;
}

export function joinFolderAndFile(folder: string, filename: string) {
  const trimmedFolder = folder.trim();
  if (!trimmedFolder) return filename;
  if (trimmedFolder.endsWith("/") || trimmedFolder.endsWith("\\")) {
    return `${trimmedFolder}${filename}`;
  }
  const separator = trimmedFolder.includes("\\") ? "\\" : "/";
  return `${trimmedFolder}${separator}${filename}`;
}

import {
  deriveLegacyAgentHotkey,
  deriveLegacyAgentMode,
  normalizeOverlayState,
  normalizeSettingsState,
} from "./normalizers";
import type {
  SpeechKitOverlayState,
  SpeechKitSettingsState,
} from "./types";

export async function fetchOverlayState() {
  const response = await fetch("/overlay/state", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`overlay state request failed: ${response.status}`);
  }
  return normalizeOverlayState(
    (await response.json()) as Partial<SpeechKitOverlayState>,
  );
}

export async function fetchSettingsState() {
  const response = await fetch("/settings/state", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`settings state request failed: ${response.status}`);
  }
  return normalizeSettingsState(
    (await response.json()) as Partial<SpeechKitSettingsState>,
  );
}

export async function saveSettingsState(nextState: SpeechKitSettingsState) {
  let overlayFreeX = nextState.overlayFreeX;
  let overlayFreeY = nextState.overlayFreeY;

  if (nextState.overlayMovable) {
    try {
      const overlayState = await fetchOverlayState();
      overlayFreeX = overlayState.positionFreeX;
      overlayFreeY = overlayState.positionFreeY;
    } catch {
      // Preserve the caller-provided snapshot when the runtime overlay state
      // cannot be refreshed.
    }
  }

  const legacyAgentMode = deriveLegacyAgentMode(
    nextState.assistHotkey,
    nextState.voiceAgentHotkey,
    nextState.activeMode,
    nextState.agentMode,
  );
  const legacyAgentHotkey = deriveLegacyAgentHotkey(
    nextState.assistHotkey,
    nextState.voiceAgentHotkey,
    nextState.activeMode,
  );
  const body = new URLSearchParams({
    overlay_enabled: nextState.overlayEnabled ? "1" : "0",
    overlay_visualizer: nextState.visualizer,
    overlay_design: nextState.design,
    assist_overlay_mode: nextState.assistOverlayMode,
    voice_agent_overlay_mode: nextState.voiceAgentOverlayMode,
    hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    dictate_hotkey: nextState.dictateHotkey ?? nextState.hotkey,
    dictate_hotkey_behavior: nextState.dictateHotkeyBehavior,
    dictate_enabled: nextState.modeEnabled.dictate ? "1" : "0",
    assist_hotkey: nextState.assistHotkey,
    assist_hotkey_behavior: nextState.assistHotkeyBehavior,
    assist_enabled: nextState.modeEnabled.assist ? "1" : "0",
    voice_agent_hotkey: nextState.voiceAgentHotkey,
    voice_agent_hotkey_behavior: nextState.voiceAgentHotkeyBehavior,
    voice_agent_close_behavior: nextState.voiceAgentCloseBehavior,
    voice_agent_profile_id: nextState.voiceAgentProfileId,
    voice_agent_refinement_prompt: nextState.voiceAgentRefinementPrompt,
    voice_agent_session_summary: nextState.voiceAgentSessionSummary ? "1" : "0",
    auto_start_on_launch: nextState.autoStartOnLaunch ? "1" : "0",
    voice_agent_enabled: nextState.modeEnabled.voice_agent ? "1" : "0",
    agent_hotkey: legacyAgentHotkey,
    agent_mode: legacyAgentMode,
    active_mode: nextState.activeMode,
    hf_model: nextState.hfModel,
    overlay_position: nextState.overlayPosition,
    overlay_movable: nextState.overlayMovable ? "1" : "0",
    overlay_free_x: String(overlayFreeX),
    overlay_free_y: String(overlayFreeY),
    store_backend: nextState.storeBackend,
    store_sqlite_path: nextState.sqlitePath,
    store_postgres_dsn: nextState.postgresDSN,
    store_save_audio: nextState.saveAudio ? "1" : "0",
    store_audio_retention_days: String(nextState.audioRetentionDays),
    store_max_audio_storage_mb: String(nextState.maxAudioStorageMB),
    model_download_dir: nextState.modelDownloadDir,
    vocabulary_dictionary: nextState.vocabularyDictionary,
    selected_audio_device_id: nextState.selectedAudioDeviceId,
    audio_device_id: nextState.selectedAudioDeviceId,
    selected_output_device_id: nextState.selectedOutputDeviceId ?? "",
    audio_output_device_id: nextState.selectedOutputDeviceId ?? "",
    dictate_primary_profile_id:
      nextState.modelSelections.dictate.primaryProfileId,
    dictate_fallback_profile_id:
      nextState.modelSelections.dictate.fallbackProfileId,
    assist_primary_profile_id:
      nextState.modelSelections.assist.primaryProfileId,
    assist_fallback_profile_id:
      nextState.modelSelections.assist.fallbackProfileId,
    voice_primary_profile_id:
      nextState.modelSelections.voice_agent.primaryProfileId,
    voice_fallback_profile_id:
      nextState.modelSelections.voice_agent.fallbackProfileId,
  });

  const response = await fetch("/settings/update", {
    method: "POST",
    body,
  });

  if (!response.ok) {
    throw new Error(`settings update failed: ${response.status}`);
  }

  const payload = (await response.json()) as { message?: string };
  if (payload.message && payload.message !== "Saved") {
    throw new Error(payload.message);
  }
  return payload.message ?? "";
}

export async function resetOverlayPosition() {
  const response = await fetch("/settings/overlay-position/reset", {
    method: "POST",
  });

  if (!response.ok) {
    throw new Error(`overlay position reset failed: ${response.status}`);
  }

  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

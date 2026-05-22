import {
  deriveLegacyAgentHotkey,
  deriveLegacyAgentMode,
  normalizeOverlayState,
  normalizeSettingsState,
} from "./normalizers";
import type {
  HomeAssistantProbeResult,
  PiperVoiceListResult,
  SpeechKitOverlayState,
  SpeechKitSettingsState,
  WakewordSelfTestReport,
} from "./types";

export async function fetchOverlayState() {
  const response = await fetch("/overlay/state", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`overlay state request failed: ${response.status}`);
  }
  return normalizeOverlayState(
    (await response.json()) as Partial<SpeechKitOverlayState>
  );
}

export async function fetchSettingsState() {
  const response = await fetch("/settings/state", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`settings state request failed: ${response.status}`);
  }
  return normalizeSettingsState(
    (await response.json()) as Partial<SpeechKitSettingsState>
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
    nextState.agentMode
  );
  const legacyAgentHotkey = deriveLegacyAgentHotkey(
    nextState.assistHotkey,
    nextState.voiceAgentHotkey,
    nextState.activeMode
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
    dictate_silence_timeout_sec: String(nextState.dictateSilenceTimeoutSec),
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
    wakeword_enabled: nextState.wakeword.enabled ? "1" : "0",
    wakeword_backend: nextState.wakeword.backend,
    wakeword_phrase_id: nextState.wakeword.phraseId,
    wakeword_default_mode: nextState.wakeword.defaultMode,
    wakeword_threshold: String(nextState.wakeword.threshold),
    wakeword_min_consecutive_frames: String(
      nextState.wakeword.minConsecutiveFrames
    ),
    wakeword_cooldown_ms: String(nextState.wakeword.cooldownMs),
    wakeword_debug_mode: nextState.wakeword.debugMode ? "1" : "0",
    homeassistant_url: nextState.homeAssistant.url,
    homeassistant_token_env: nextState.homeAssistant.tokenEnv,
    homeassistant_language: nextState.homeAssistant.language,
    piper_enabled: nextState.piperTTS.enabled ? "1" : "0",
    piper_binary: nextState.piperTTS.binary,
    piper_voice_dir: nextState.piperTTS.voiceDir,
    piper_timeout_sec: String(nextState.piperTTS.timeoutSec),
  });

  // Encode per-locale Piper defaults as piper_default_voice[<locale>]=…
  for (const [locale, voice] of Object.entries(
    nextState.piperTTS.defaultVoices ?? {}
  )) {
    body.append(`piper_default_voice[${locale.toLowerCase()}]`, voice ?? "");
  }

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

// ── v0.38.2: HomeAssistant probe + Piper voice listing ──────────────────────

// probeHomeAssistant tests the configured HomeAssistant base URL +
// token by hitting /api/. Used by the Settings UI "Test connection"
// button. Pass the in-flight draft values so the user can verify a
// new URL/token-env before saving.
export async function probeHomeAssistant(
  draft?: { url?: string; tokenEnv?: string }
): Promise<HomeAssistantProbeResult> {
  const body = new URLSearchParams();
  if (draft?.url) body.set("homeassistant_url", draft.url);
  if (draft?.tokenEnv) body.set("homeassistant_token_env", draft.tokenEnv);
  const response = await fetch("/settings/homeassistant/test", {
    method: "POST",
    body,
  });
  if (!response.ok) {
    throw new Error(
      `home_assistant probe failed: ${response.status} ${response.statusText}`
    );
  }
  return (await response.json()) as HomeAssistantProbeResult;
}

// fetchPiperVoices returns the live voice-directory scan from the
// Go backend. Pass an in-flight voice_dir to preview a candidate
// folder before saving the settings.
export async function fetchPiperVoices(voiceDir?: string): Promise<PiperVoiceListResult> {
  const url = voiceDir
    ? `/api/tts/piper/voices?voice_dir=${encodeURIComponent(voiceDir)}`
    : "/api/tts/piper/voices";
  const response = await fetch(url, { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`piper voice list failed: ${response.status}`);
  }
  return (await response.json()) as PiperVoiceListResult;
}

// WakewordState mirrors the JSON returned by /app/wakeword/state and the
// enable/disable endpoints. `listening` reflects whether the sidecar
// process is actually running right now (true when the runtime is non-nil
// in the desktop state); `status` is the human-readable last status the
// adapter wrote (e.g. "Listening for Hey Siri" or "Disabled").
export type WakewordState = {
  enabled: boolean;
  listening: boolean;
  backend: string;
  phraseId: string;
  defaultMode: "voice_agent" | "assist" | "dictate" | string;
  threshold?: number;
  status: string;
};

export async function fetchWakewordState(): Promise<WakewordState> {
  const response = await fetch("/app/wakeword/state");
  if (!response.ok) {
    throw new Error(`wake-word state failed: ${response.status}`);
  }
  return (await response.json()) as WakewordState;
}

// enableWakeword is the one-click activation path used by the onboarding
// wizard. The backend picks sensible defaults for any field omitted from
// the request body (backend="sherpa_kws", phraseId="hey_quby",
// defaultMode="voice_agent"),
// persists the config, and respawns the wake-word sidecar before
// returning the resulting state. Callers should poll `listening` for a
// few seconds after the response to confirm the sidecar fully booted.
export async function enableWakeword(opts?: {
  backend?: string;
  phraseId?: string;
  defaultMode?: WakewordState["defaultMode"];
  threshold?: number;
}): Promise<WakewordState> {
  const response = await fetch("/app/wakeword/enable", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts ?? {}),
  });
  if (!response.ok) {
    const detail = await response.text().catch(() => "");
    throw new Error(`wake-word enable failed: ${response.status}: ${detail}`);
  }
  return (await response.json()) as WakewordState;
}

export async function disableWakeword(): Promise<WakewordState> {
  const response = await fetch("/app/wakeword/disable", { method: "POST" });
  if (!response.ok) {
    throw new Error(`wake-word disable failed: ${response.status}`);
  }
  return (await response.json()) as WakewordState;
}

// runWakewordSelfTest POSTs to /api/wakeword/selftest, which records a
// short sample of audio through the currently configured capture device
// and returns a report with peak level + resolved device name + actionable
// advice. Used by the Wake-word settings panel "Test microphone" button
// to confirm the mic is actually capturing before blaming the detector.
export async function runWakewordSelfTest(): Promise<WakewordSelfTestReport> {
  const response = await fetch("/api/wakeword/selftest", { method: "POST" });
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`wake-word self-test failed: ${response.status}: ${body}`);
  }
  return (await response.json()) as WakewordSelfTestReport;
}

// ── v0.37.6: Wake-word activation management ─────────────────────────────────

// LocalWakewordActivation mirrors the Go-side struct in
// cmd/speechkit/routes_wakeword_activations.go. It's the JSON shape the
// "Training data" panel renders; one entry per locally captured detection.
export type LocalWakewordActivationLabel = "" | "correct" | "false_positive";

export type LocalWakewordActivation = {
  id: string;
  phrase_id: string;
  phrase: string;
  backend: string;
  score: number;
  captured_at: string;
  pre_roll_ms: number;
  post_roll_ms: number;
  sample_rate: number;
  audio_bytes: number;
  label: LocalWakewordActivationLabel;
  uploaded: boolean;
  audio_url: string;
};

export type WakewordActivationsListResponse = {
  activations: LocalWakewordActivation[];
  count: number;
  dir: string;
  enabled: boolean;
};

// listWakewordActivations returns all locally captured activations the
// sidecar has saved under <local_capture_dir>. Returns an empty list
// when capture is disabled or the dir has not been created yet — the
// `enabled` flag in the response tells the UI which empty-state to
// render (a privacy hint vs. a "no captures yet" hint).
export async function listWakewordActivations(): Promise<WakewordActivationsListResponse> {
  const response = await fetch("/api/wakeword/activations", { cache: "no-store" });
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`list activations failed: ${response.status}: ${body}`);
  }
  return (await response.json()) as WakewordActivationsListResponse;
}

// updateWakewordActivationLabel writes a new `label` value into the
// sidecar's JSON and resets the `uploaded` flag so the next background
// uploader tick re-syncs the labelled clip to the server (when remote
// upload is enabled). Empty `label` clears the label.
export async function updateWakewordActivationLabel(
  id: string,
  label: LocalWakewordActivationLabel,
): Promise<void> {
  const response = await fetch(`/api/wakeword/activations/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ label }),
  });
  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`label update failed: ${response.status}: ${body}`);
  }
}

// deleteWakewordActivation removes both the .wav and the .json sidecar
// from disk. Idempotent — a 404 for an already-deleted clip is treated
// as success so the UI can hide it from the list either way.
export async function deleteWakewordActivation(id: string): Promise<void> {
  const response = await fetch(`/api/wakeword/activations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!response.ok && response.status !== 404) {
    const body = await response.text().catch(() => "");
    throw new Error(`delete failed: ${response.status}: ${body}`);
  }
}

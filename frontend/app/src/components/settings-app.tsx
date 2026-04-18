import { useCallback, useEffect, useRef, useState } from "react";

import { MicSelector } from "@/components/ui/mic-selector";
import {
  cancelModelDownload,
  clearProviderCredential,
  defaultSettingsState,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  fetchOverlayState,
  fetchModelProfiles,
  fetchSettingsState,
  resetOverlayPosition,
  saveProviderCredential,
  saveSettingsState,
  selectDownloadedModel,
  startModelDownload,
  testProviderCredential,
  type DownloadItem,
  type DownloadJob,
  type HotkeyBehavior,
  type ModeModelSelectionState,
  type ProviderCredentialState,
  type SpeechKitSettingsState,
} from "@/lib/speechkit";

type Tab = "general" | "stt" | "assist" | "realtime_voice";
export type SettingsTab = Tab;

const HOTKEY_SUFFIX_KEYS = ["", "d", "j", "k", "v", "space"] as const;
const HOTKEY_BASE_OPTIONS = [
  { value: "win+alt", label: "Win + Alt" },
  { value: "ctrl+win", label: "Ctrl + Win" },
  { value: "ctrl+shift", label: "Ctrl + Shift" },
] as const;
const HOTKEY_BEHAVIOR_OPTIONS = [
  { value: "push_to_talk", label: "Hold to talk" },
  { value: "toggle", label: "Toggle on press" },
] as const;
const MODE_DEFAULT_BASES = {
  dictate: "win+alt",
  assist: "ctrl+win",
  voice_agent: "ctrl+shift",
} as const;
const MODE_HOTKEY_FIELDS = {
  dictate: "dictateHotkey",
  assist: "assistHotkey",
  voice_agent: "voiceAgentHotkey",
} as const;
const MODE_HOTKEY_BEHAVIOR_FIELDS = {
  dictate: "dictateHotkeyBehavior",
  assist: "assistHotkeyBehavior",
  voice_agent: "voiceAgentHotkeyBehavior",
} as const;
const MODE_SELECTION_KEYS = {
  stt: "dictate",
  assist: "assist",
  realtime_voice: "voice_agent",
} as const;

type ConfigurableMode = keyof typeof MODE_HOTKEY_FIELDS;
type HotkeyBase = (typeof HOTKEY_BASE_OPTIONS)[number]["value"];
type HotkeySuffix = (typeof HOTKEY_SUFFIX_KEYS)[number];

function parseModeHotkeyValue(
  value: string,
  fallbackBase: HotkeyBase,
): { base: HotkeyBase; suffix: HotkeySuffix } {
  const normalized = value.trim().toLowerCase();
  for (const option of HOTKEY_BASE_OPTIONS) {
    if (normalized === option.value) {
      return { base: option.value, suffix: "" };
    }
    const prefix = `${option.value}+`;
    if (normalized.startsWith(prefix)) {
      const suffix = normalized.slice(prefix.length) as HotkeySuffix;
      if (HOTKEY_SUFFIX_KEYS.includes(suffix)) {
        return { base: option.value, suffix };
      }
    }
  }
  return { base: fallbackBase, suffix: "" };
}

function buildModeHotkey(base: HotkeyBase, suffix: HotkeySuffix) {
  return suffix ? `${base}+${suffix}` : base;
}

function deriveAvailableModes(settings: SpeechKitSettingsState) {
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

function reconcileSettingsState(
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

function providerSecretNoun(provider?: string) {
  return provider === "huggingface" ? "token" : "key";
}

function providerCredentialCopy(
  profileName: string,
  credential: ProviderCredentialState,
) {
  const noun = providerSecretNoun(credential.provider);
  const credentialLabel = `${credential.label} ${noun}`;
  return {
    title: `Add ${credentialLabel}`,
    inputLabel: `${profileName} ${credentialLabel}`,
    placeholder: credential.envName || (noun === "token" ? "Token" : "API key"),
    saveLabel: `Save ${noun}`,
    neededLabel: `${credentialLabel} needed`,
    unlockLabel: `Add the ${noun} above to unlock this model.`,
  };
}

export function SettingsApp({ initialTab = "general" }: { initialTab?: Tab }) {
  const [settings, setSettings] = useState(defaultSettingsState);
  const [providerTokens, setProviderTokens] = useState<Record<string, string>>(
    {},
  );
  const [providerBusy, setProviderBusy] = useState<Record<string, boolean>>({});
  const [loaded, setLoaded] = useState(false);
  const [toast, setToast] = useState("");
  const [tab, setTab] = useState<Tab>(initialTab);
  const [dlCatalog, setDlCatalog] = useState<DownloadItem[]>([]);
  const [dlJobs, setDlJobs] = useState<DownloadJob[]>([]);
  const [confirmItem, setConfirmItem] = useState<DownloadItem | null>(null);
  const [dlBusy, setDlBusy] = useState(false);
  const saveTimer = useRef<number | null>(null);
  const toastTimer = useRef<number | null>(null);

  const loadSettings = useCallback(async () => {
    const [state, profiles] = await Promise.all([
      fetchSettingsState(),
      fetchModelProfiles().catch(() => []),
    ]);
    setSettings(
      reconcileSettingsState({
        ...state,
        profiles: state.profiles?.length ? state.profiles : profiles,
      }),
    );
  }, []);

  useEffect(() => {
    let active = true;
    void loadSettings()
      .then(() => {
        if (!active) return;
        setLoaded(true);
      })
      .catch(() => {
        if (!active) return;
        setLoaded(true);
      });
    fetchDownloadCatalog()
      .then(setDlCatalog)
      .catch(() => {});
    fetchDownloadJobs()
      .then(setDlJobs)
      .catch(() => {});
    return () => {
      active = false;
      if (saveTimer.current) window.clearTimeout(saveTimer.current);
      if (toastTimer.current) window.clearTimeout(toastTimer.current);
    };
  }, [loadSettings]);

  useEffect(() => {
    const refresh = () => {
      void loadSettings().catch(() => {});
    };
    const refreshListener = refresh as EventListener;
    window.addEventListener("speechkit:dashboard-show", refreshListener);
    return () => {
      window.removeEventListener("speechkit:dashboard-show", refreshListener);
    };
  }, [loadSettings]);

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);

  useEffect(() => {
    const hasActive = dlJobs.some(
      (j) => j.status === "pending" || j.status === "running",
    );
    if (!hasActive) return;
    const timer = setInterval(() => {
      fetchDownloadJobs()
        .then((jobs) => {
          setDlJobs(jobs);
          const wasRunning = dlJobs.some(
            (j) => j.status === "running" || j.status === "pending",
          );
          const nowDone = jobs.every(
            (j) =>
              j.status === "done" ||
              j.status === "failed" ||
              j.status === "cancelled",
          );
          if (wasRunning && nowDone) {
            fetchDownloadCatalog()
              .then(setDlCatalog)
              .catch(() => {});
          }
        })
        .catch(() => {});
    }, 2000);
    return () => clearInterval(timer);
  }, [dlJobs]);

  const showToast = (message: string) => {
    if (toastTimer.current) window.clearTimeout(toastTimer.current);
    setToast(message);
    toastTimer.current = window.setTimeout(() => setToast(""), 1400);
  };

  const queueSave = (next: SpeechKitSettingsState, delay: number) => {
    setSettings(next);
    if (!loaded) return;
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
    const waitingForPostgresDSN =
      next.storeBackend === "postgres" &&
      !next.postgresConfigured &&
      next.postgresDSN.trim().length === 0;
    if (waitingForPostgresDSN) return;
    saveTimer.current = window.setTimeout(async () => {
      try {
        const message = await saveSettingsState(next);
        showToast(message || "Saved");
      } catch (err) {
        showToast(err instanceof Error ? err.message : "Save failed");
      }
    }, delay);
  };

  const updateSettings = (
    patch: Partial<SpeechKitSettingsState>,
    delay = 0,
  ) => {
    queueSave(reconcileSettingsState({ ...settings, ...patch }), delay);
  };

  const updateModeHotkey = (mode: ConfigurableMode, value: string) => {
    const trimmedValue = value.trim();
    const patch: Partial<SpeechKitSettingsState> = {
      [MODE_HOTKEY_FIELDS[mode]]: trimmedValue,
      modeEnabled: {
        ...settings.modeEnabled,
        [mode]: settings.modeEnabled[mode] && trimmedValue.length > 0,
      },
    };

    if (mode === "dictate") {
      patch.hotkey = trimmedValue;
    }

    updateSettings(patch);
  };

  const updateModeHotkeyBehavior = (
    mode: ConfigurableMode,
    value: HotkeyBehavior,
  ) => {
    updateSettings({
      [MODE_HOTKEY_BEHAVIOR_FIELDS[mode]]: value,
    } as Partial<SpeechKitSettingsState>);
  };

  const updateModelSelection = (
    mode: keyof SpeechKitSettingsState["modelSelections"],
    field: keyof ModeModelSelectionState,
    value: string,
  ) => {
    updateSettings({
      modelSelections: {
        ...settings.modelSelections,
        [mode]: normalizeModeSelectionUpdate(
          settings.modelSelections[mode],
          field,
          value,
        ),
      },
    });
  };

  const toggleModeEnabled = (mode: ConfigurableMode) => {
    const field = MODE_HOTKEY_FIELDS[mode];
    const currentValue = settings[field].trim();
    const nextEnabled = !settings.modeEnabled[mode];
    const fallbackHotkey = currentValue || MODE_DEFAULT_BASES[mode];

    updateSettings({
      [field]: fallbackHotkey,
      modeEnabled: {
        ...settings.modeEnabled,
        [mode]: nextEnabled,
      },
    } as Partial<SpeechKitSettingsState>);
  };

  const tokenStatusLabel = (cred: ProviderCredentialState) => {
    const noun = providerSecretNoun(cred.provider);
    switch (cred.source) {
      case "user":
        return `User ${noun} active`;
      case "install":
        return `Install ${noun} active`;
      case "env":
        return `Environment ${noun} active`;
      default:
        return `No ${noun} configured`;
    }
  };

  const postgresReady =
    settings.postgresConfigured || settings.postgresDSN.trim().length > 0;

  const handleSaveProviderCredential = async (provider: string) => {
    const token = (providerTokens[provider] ?? "").trim();
    const label = settings.providerCredentials?.[provider]?.label ?? "API";
    const noun = providerSecretNoun(provider);
    if (!token) {
      showToast(`${label} ${noun} required`);
      return;
    }
    setProviderBusy((b) => ({ ...b, [provider]: true }));
    try {
      const result = await saveProviderCredential(provider, token);
      setProviderTokens((t) => ({ ...t, [provider]: "" }));
      showToast(result.message ?? "Saved");
      await loadSettings();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Save failed");
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }));
    }
  };

  const handleClearProviderCredential = async (provider: string) => {
    setProviderBusy((b) => ({ ...b, [provider]: true }));
    try {
      const result = await clearProviderCredential(provider);
      setProviderTokens((t) => ({ ...t, [provider]: "" }));
      showToast(result.message ?? "Cleared");
      await loadSettings();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Clear failed");
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }));
    }
  };

  const handleTestProviderCredential = async (provider: string) => {
    const token = (providerTokens[provider] ?? "").trim();
    const storedCredential = settings.providerCredentials?.[provider];
    if (!token && !storedCredential?.available) {
      showToast(`No ${providerSecretNoun(provider)} configured`);
      return;
    }
    setProviderBusy((b) => ({ ...b, [provider]: true }));
    try {
      const result = await testProviderCredential(provider, token);
      showToast(result.message ?? "Key valid");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Test failed");
    } finally {
      setProviderBusy((b) => ({ ...b, [provider]: false }));
    }
  };

  const handleSaveCurrentOverlaySpot = async () => {
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
    try {
      const overlayState = await fetchOverlayState();
      const next = reconcileSettingsState({
        ...settings,
        overlayMovable: true,
        overlayFreeX: overlayState.positionFreeX,
        overlayFreeY: overlayState.positionFreeY,
      });
      setSettings(next);
      const message = await saveSettingsState(next);
      showToast(message || "Saved");
      await loadSettings();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Save failed");
    }
  };

  const handleResetOverlaySpot = async () => {
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
    try {
      const message = await resetOverlayPosition();
      showToast(message || "Saved");
      await loadSettings();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Reset failed");
    }
  };

  const hasSavedOverlaySpot =
    settings.overlayFreeX !== 0 || settings.overlayFreeY !== 0;

  return (
    <div
      data-testid="settings-layout"
      className="flex h-full min-h-0 min-w-0 bg-[color:var(--sk-surface-0)] text-[13px] text-[color:var(--sk-text)]"
    >
      {/* Settings sub-nav */}
      <div className="w-45 shrink-0 overflow-y-auto border-r border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-1)] px-3 py-6">
        <h2 className="mb-5 px-3 text-xs font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
          Settings
        </h2>
        <nav className="space-y-0.5">
          <TabBtn active={tab === "general"} onClick={() => setTab("general")}>
            General
          </TabBtn>
          <TabBtn active={tab === "stt"} onClick={() => setTab("stt")}>
            Transcribe
          </TabBtn>
          <TabBtn active={tab === "assist"} onClick={() => setTab("assist")}>
            Assist
          </TabBtn>
          <TabBtn
            active={tab === "realtime_voice"}
            onClick={() => setTab("realtime_voice")}
          >
            Voice Agent
          </TabBtn>
        </nav>
      </div>

      {/* Content */}
      <div
        data-testid="settings-scroll-region"
        className="min-h-0 flex-1 overflow-y-auto px-8 py-6"
      >
        {/* General tab — two-column layout */}
        {tab === "general" && (
          <div className="grid grid-cols-2 gap-x-10 gap-y-5">
            {/* Left column: Launch · Microphone */}
            <div className="flex flex-col gap-5">
              <Section title="Startup">
                <Row
                  label="Auto-start on app launch"
                  on={settings.autoStartOnLaunch}
                  onToggle={() =>
                    updateSettings({
                      autoStartOnLaunch: !settings.autoStartOnLaunch,
                    })
                  }
                />
                <p className="mt-2 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]/80">
                  Starts the configured launch session automatically when
                  SpeechKit opens. Keep mode-specific controls on their own tab.
                </p>
              </Section>

              <Section title="Microphone">
                <MicSelector
                  value={settings.selectedAudioDeviceId}
                  onValueChange={(deviceId) =>
                    updateSettings({ selectedAudioDeviceId: deviceId })
                  }
                  className="w-full"
                />
              </Section>
            </div>

            {/* Right column: Overlay · Storage */}
            <div className="flex flex-col gap-5">
              <Section title="Overlay">
                <Row
                  label="Show overlay"
                  on={settings.overlayEnabled}
                  onToggle={() =>
                    updateSettings({ overlayEnabled: !settings.overlayEnabled })
                  }
                />
                {settings.overlayEnabled && (
                  <div className="mt-2 flex flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="mr-1 text-[11px] text-[color:var(--sk-text-muted)]">
                        Style
                      </span>
                      <Chip
                        active={settings.visualizer === "pill"}
                        onClick={() => updateSettings({ visualizer: "pill" })}
                      >
                        Default{" "}
                        <span className="ml-1 text-[10px] opacity-50">
                          (Pill)
                        </span>
                      </Chip>
                      <Chip
                        active={settings.visualizer === "circle"}
                        onClick={() => updateSettings({ visualizer: "circle" })}
                      >
                        Focus{" "}
                        <span className="ml-1 text-[10px] opacity-50">
                          (Dot)
                        </span>
                      </Chip>
                      {settings.visualizer === "pill" && (
                        <>
                          <span className="mx-1 text-[color:var(--sk-border)]">
                            |
                          </span>
                          <Chip
                            active={settings.design === "default"}
                            onClick={() =>
                              updateSettings({ design: "default" })
                            }
                          >
                            Default
                          </Chip>
                          <Chip
                            active={settings.design === "kombify"}
                            onClick={() =>
                              updateSettings({ design: "kombify" })
                            }
                          >
                            kombify
                          </Chip>
                        </>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="mr-1 text-[11px] text-[color:var(--sk-text-muted)]">
                        Position
                      </span>
                      {(["top", "bottom", "left", "right"] as const).map(
                        (pos) => (
                          <Chip
                            key={pos}
                            active={settings.overlayPosition === pos}
                            onClick={() =>
                              updateSettings({ overlayPosition: pos })
                            }
                          >
                            {pos.charAt(0).toUpperCase() + pos.slice(1)}
                          </Chip>
                        ),
                      )}
                    </div>
                    <Row
                      label="Movable overlay"
                      on={settings.overlayMovable}
                      onToggle={() =>
                        updateSettings({
                          overlayMovable: !settings.overlayMovable,
                        })
                      }
                    />
                    {settings.overlayMovable && (
                      <div className="flex flex-col gap-2">
                        {settings.visualizer === "pill" && (
                          <p className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                            Drag the center bubble inside the pill panel to
                            place it anywhere on the desktop.
                          </p>
                        )}
                        <div className="rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] px-3 py-2.5">
                          <p className="text-[11px] text-[color:var(--sk-text-muted)]">
                            {hasSavedOverlaySpot
                              ? `Saved spot: X ${settings.overlayFreeX}, Y ${settings.overlayFreeY}`
                              : "No custom spot saved yet. The overlay falls back to the selected edge until you save the current spot."}
                          </p>
                          <div className="mt-2 flex flex-wrap gap-2">
                            <button
                              type="button"
                              onClick={() =>
                                void handleSaveCurrentOverlaySpot()
                              }
                              className="sk-secondary-button rounded-full px-3 py-1.5 text-[11px] font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
                            >
                              Save current spot
                            </button>
                            <button
                              type="button"
                              onClick={() => void handleResetOverlaySpot()}
                              disabled={!hasSavedOverlaySpot}
                              className={[
                                "rounded-full px-3 py-1.5 text-[11px] font-medium transition-colors",
                                hasSavedOverlaySpot
                                  ? "sk-secondary-button hover:bg-[color:var(--sk-surface-3)]"
                                  : "cursor-not-allowed border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] text-[color:var(--sk-text-subtle)]",
                              ].join(" ")}
                            >
                              Reset saved spot
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </Section>

              <Section title="Storage">
                <div className="flex flex-wrap gap-1.5">
                  <Chip
                    active={settings.storeBackend === "sqlite"}
                    onClick={() => updateSettings({ storeBackend: "sqlite" })}
                  >
                    SQLite
                  </Chip>
                  <Chip
                    active={settings.storeBackend === "postgres"}
                    onClick={() => updateSettings({ storeBackend: "postgres" })}
                  >
                    PostgreSQL
                  </Chip>
                </div>
                <p
                  className={[
                    "mt-1.5 rounded border px-2.5 py-1.5 text-[11px]",
                    settings.storeBackend === "postgres" && !postgresReady
                      ? "border-orange-500/20 bg-orange-500/5 text-orange-200/70"
                      : "border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] text-[color:var(--sk-text-muted)]",
                  ].join(" ")}
                >
                  {settings.storeBackend === "sqlite"
                    ? "SQLite keeps metadata in the local SpeechKit app data folder."
                    : postgresReady
                      ? "PostgreSQL metadata backend is configured. Restart the app after changes."
                      : "Add a PostgreSQL connection string before switching the metadata backend."}
                </p>
                {settings.storeBackend === "sqlite" ? (
                  <input
                    id="sqlite-path-input"
                    aria-label="SQLite path"
                    value={settings.sqlitePath}
                    onChange={(e) =>
                      updateSettings({ sqlitePath: e.target.value }, 350)
                    }
                    placeholder="%APPDATA%/SpeechKit/feedback.db"
                    className="sk-input mt-1.5 h-8 w-full rounded px-2.5 text-xs"
                  />
                ) : (
                  <input
                    id="postgres-dsn-input"
                    aria-label="PostgreSQL connection string"
                    value={settings.postgresDSN}
                    onChange={(e) =>
                      updateSettings({ postgresDSN: e.target.value }, 350)
                    }
                    placeholder="postgres://user:password@host:5432/speechkit?sslmode=disable"
                    className="sk-input mt-1.5 h-8 w-full rounded px-2.5 text-xs"
                  />
                )}
                <div className="mt-2.5">
                  <Row
                    label="Save raw audio locally"
                    on={settings.saveAudio}
                    onToggle={() =>
                      updateSettings({ saveAudio: !settings.saveAudio })
                    }
                  />
                </div>
                <div className="mt-2 grid grid-cols-2 gap-3">
                  <div>
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
                      Audio retention
                    </div>
                    <select
                      id="audio-retention-select"
                      aria-label="Audio retention"
                      value={String(settings.audioRetentionDays)}
                      onChange={(e) =>
                        updateSettings({
                          audioRetentionDays: Number(e.target.value),
                        })
                      }
                      className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                    >
                      <option value="0">No automatic deletion</option>
                      <option value="1">1 day</option>
                      <option value="7">7 days</option>
                      <option value="30">30 days</option>
                      <option value="90">90 days</option>
                    </select>
                  </div>
                  <div>
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
                      Max storage (MB)
                    </div>
                    <input
                      id="max-audio-storage-input"
                      aria-label="Max local audio storage (MB)"
                      type="number"
                      min="0"
                      value={String(settings.maxAudioStorageMB)}
                      onChange={(e) => {
                        const nextValue = Number.parseInt(e.target.value, 10);
                        if (Number.isNaN(nextValue) || nextValue < 0) return;
                        updateSettings({ maxAudioStorageMB: nextValue }, 250);
                      }}
                      className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                    />
                  </div>
                </div>
              </Section>
            </div>

            {/* Vocabulary — full width */}
            <div className="col-span-2">
              <Section title="Vocabulary">
                <textarea
                  id="vocabulary-dictionary-input"
                  aria-label="Vocabulary dictionary"
                  value={settings.vocabularyDictionary}
                  onChange={(e) =>
                    updateSettings(
                      { vocabularyDictionary: e.target.value },
                      250,
                    )
                  }
                  rows={3}
                  placeholder={"kombi fire => Kombify\nAcmeOS\nGemma"}
                  className="w-full rounded border border-[#484555] bg-[#0e0e13] px-3 py-2 text-xs leading-6 text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
                />
                <p className="mt-1 text-[11px] text-[#938ea1]/70">
                  One term per line. Use <code>spoken =&gt; canonical</code> for
                  corrections, or a bare term to bias transcription.
                </p>
              </Section>
            </div>
          </div>
        )}

        {tab === "stt" && (
          <div className="mb-5">
            <ModeSection
              title="Transcribe Controls"
              testId="transcribe-mode-controls"
            >
              <HotkeyPicker
                label="Dictate hotkey"
                enabled={settings.modeEnabled.dictate}
                value={settings.dictateHotkey}
                behavior={settings.dictateHotkeyBehavior}
                defaultBase={MODE_DEFAULT_BASES.dictate}
                onToggleEnabled={() => toggleModeEnabled("dictate")}
                onChange={(value) => updateModeHotkey("dictate", value)}
                onChangeBehavior={(value) =>
                  updateModeHotkeyBehavior("dictate", value)
                }
              />
            </ModeSection>
          </div>
        )}

        {tab === "assist" && (
          <div className="mb-5">
            <ModeSection title="Assist Controls" testId="assist-mode-controls">
              <HotkeyPicker
                label="Assist hotkey"
                enabled={settings.modeEnabled.assist}
                value={settings.assistHotkey}
                behavior={settings.assistHotkeyBehavior}
                defaultBase={MODE_DEFAULT_BASES.assist}
                onToggleEnabled={() => toggleModeEnabled("assist")}
                onChange={(value) => updateModeHotkey("assist", value)}
                onChangeBehavior={(value) =>
                  updateModeHotkeyBehavior("assist", value)
                }
              />
            </ModeSection>
          </div>
        )}

        {tab === "realtime_voice" && (
          <div className="mb-5 grid gap-5 lg:grid-cols-2">
            <ModeSection
              title="Voice Agent Controls"
              testId="voice-agent-mode-controls"
            >
              <HotkeyPicker
                label="Voice Agent hotkey"
                enabled={settings.modeEnabled.voice_agent}
                value={settings.voiceAgentHotkey}
                behavior={settings.voiceAgentHotkeyBehavior}
                defaultBase={MODE_DEFAULT_BASES.voice_agent}
                onToggleEnabled={() => toggleModeEnabled("voice_agent")}
                onChange={(value) => updateModeHotkey("voice_agent", value)}
                onChangeBehavior={(value) =>
                  updateModeHotkeyBehavior("voice_agent", value)
                }
              />
            </ModeSection>

            <ModeSection title="Conversation">
              <div className="flex flex-col gap-2">
                <p className="text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
                  Add your personal preferences on top of the built-in
                  Voice Agent framework, then choose what the transcript
                  window close button should do. Minimise always keeps the
                  current conversation available in the taskbar.
                </p>
                <div className="grid gap-3">
                  <label className="flex flex-col gap-1.5">
                    <span className="text-[11px] font-medium text-[color:var(--sk-text)]">
                      Personal refinement prompt
                    </span>
                    <textarea
                      aria-label="Voice Agent personal refinement prompt"
                      value={settings.voiceAgentRefinementPrompt}
                      onChange={(e) =>
                        updateSettings(
                          { voiceAgentRefinementPrompt: e.target.value },
                          250,
                        )
                      }
                      rows={4}
                      placeholder="Prefer concise answers, call me Mako, and keep follow-up questions short."
                      className="sk-input min-h-24 w-full rounded-[18px] px-3 py-2 text-xs leading-6"
                    />
                    <span className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                      This adds personal preferences on top of the internal
                      framework prompt without replacing it.
                    </span>
                  </label>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Chip
                    active={settings.voiceAgentCloseBehavior === "continue"}
                    onClick={() =>
                      updateSettings({ voiceAgentCloseBehavior: "continue" })
                    }
                  >
                    Minimise and keep chat
                  </Chip>
                  <Chip
                    active={settings.voiceAgentCloseBehavior === "new_chat"}
                    onClick={() =>
                      updateSettings({ voiceAgentCloseBehavior: "new_chat" })
                    }
                  >
                    End chat on close
                  </Chip>
                </div>
                <p className="text-[11px] text-[color:var(--sk-text-muted)]/80">
                  Use the close button to either park the current session or
                  reset it cleanly before the next start.
                </p>
              </div>
            </ModeSection>
          </div>
        )}

        {(tab === "stt" || tab === "assist" || tab === "realtime_voice") && (
          <ModelPanel
            modality={tab}
            settings={settings}
            showToast={showToast}
            providerTokens={providerTokens}
            setProviderTokens={setProviderTokens}
            providerBusy={providerBusy}
            tokenStatusLabel={tokenStatusLabel}
            onSaveCredential={handleSaveProviderCredential}
            onClearCredential={handleClearProviderCredential}
            onTestCredential={handleTestProviderCredential}
            onUpdateSelection={updateModelSelection}
            onRefreshSettings={loadSettings}
            dlCatalog={dlCatalog}
            setDlCatalog={setDlCatalog}
            dlJobs={dlJobs}
            setDlJobs={setDlJobs}
            confirmItem={confirmItem}
            setConfirmItem={setConfirmItem}
            dlBusy={dlBusy}
            setDlBusy={setDlBusy}
          />
        )}

        {/* Toast */}
        <div
          className={[
            "pointer-events-none fixed top-4 right-4 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-300 transition-all",
            toast ? "translate-y-0 opacity-100" : "-translate-y-2 opacity-0",
          ].join(" ")}
        >
          {toast || "\u00A0"}
        </div>
      </div>
    </div>
  );
}

function TabBtn({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        "w-full rounded-2xl px-3 py-2.5 text-left text-sm transition-all",
        active
          ? "border border-[color:var(--sk-accent)]/18 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)] font-semibold"
          : "border border-transparent text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-panel-border)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="sk-panel rounded-[24px] p-5">
      <div className="mb-1.5">
        <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
          {title}
        </span>
      </div>
      {children}
    </section>
  );
}

function ModeSection({
  title,
  children,
  testId,
}: {
  title: string;
  children: React.ReactNode;
  testId?: string;
}) {
  return (
    <section
      data-testid={testId}
      className="rounded-[24px] bg-transparent px-1 py-1"
    >
      <div className="mb-4 border-b border-[color:var(--sk-shell-divider)]/85 pb-3">
        <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
          {title}
        </span>
      </div>
      {children}
    </section>
  );
}

function HotkeyPicker({
  label,
  enabled,
  value,
  behavior,
  defaultBase,
  onToggleEnabled,
  onChange,
  onChangeBehavior,
}: {
  label: string;
  enabled: boolean;
  value: string;
  behavior: HotkeyBehavior;
  defaultBase: HotkeyBase;
  onToggleEnabled: () => void;
  onChange: (value: string) => void;
  onChangeBehavior: (value: HotkeyBehavior) => void;
}) {
  const { base, suffix } = parseModeHotkeyValue(value, defaultBase);

  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-[color:var(--sk-text)]">
        {label}
      </div>
      <div className="mb-2">
        <Row
          label={`Enable ${label}`}
          on={enabled}
          onToggle={onToggleEnabled}
        />
      </div>
      <div className="mb-2 flex flex-wrap items-center gap-1.5">
        {HOTKEY_BEHAVIOR_OPTIONS.map((option) => (
          <Chip
            key={option.value}
            active={behavior === option.value}
            ariaLabel={`${label} ${option.label}`}
            onClick={() => onChangeBehavior(option.value)}
          >
            {option.label}
          </Chip>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {HOTKEY_BASE_OPTIONS.map((option) => (
          <Chip
            key={option.value}
            active={base === option.value}
            ariaLabel={`${label} ${option.label}`}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </Chip>
        ))}
        <select
          aria-label={`${label} suffix`}
          value={suffix}
          onChange={(event) =>
            onChange(buildModeHotkey(base, event.target.value as HotkeySuffix))
          }
          className="sk-input h-8 rounded-lg px-2.5 text-xs font-medium"
        >
          {HOTKEY_SUFFIX_KEYS.map((key) => (
            <option key={key || "none"} value={key}>
              {key === ""
                ? "None"
                : key === "space"
                  ? "Space"
                  : key.toUpperCase()}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

const PROVIDER_FOR_EXECUTION_MODE: Record<string, string | undefined> = {
  hf_routed: "huggingface",
  hf_inference: "huggingface",
  openai_api: "openai",
  groq_api: "groq",
  google_api: "google",
  openrouter_api: "openrouter",
};

type ModalityTabKey = "stt" | "assist" | "realtime_voice";

function limitProfilesToVisibleOptions(
  profiles: SpeechKitSettingsState["profiles"],
  preferredProfileIDs: string[] = [],
  maxOptions = 3,
) {
  const list = profiles ?? [];
  if (list.length <= maxOptions) return list;
  const next = list.slice(0, maxOptions);
  const preferred = preferredProfileIDs
    .map((profileID) => list.find((profile) => profile.id === profileID))
    .filter((profile): profile is NonNullable<typeof profile> =>
      Boolean(profile),
    );

  for (const profile of preferred) {
    if (next.some((entry) => entry.id === profile.id)) {
      continue;
    }
    next.splice(Math.max(next.length - 1, 0), 1, profile);
  }

  return next;
}

function sourceBadge(
  profile: NonNullable<SpeechKitSettingsState["profiles"]>[number],
) {
  switch (profile.executionMode) {
    case "local":
    case "ollama_local":
      return {
        label: "local",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
    case "hf_routed":
    case "hf_inference":
      return {
        label: "hugging face",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
    default:
      return {
        label: "api key",
        className:
          "bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-muted)]",
      };
  }
}

function normalizeModeSelectionUpdate(
  current: ModeModelSelectionState,
  field: keyof ModeModelSelectionState,
  value: string,
) {
  const next = {
    ...current,
    [field]: value.trim(),
  };

  if (
    next.primaryProfileId !== "" &&
    next.primaryProfileId === next.fallbackProfileId
  ) {
    next.fallbackProfileId = "";
  }

  return next;
}

function ModelPanel({
  modality,
  settings,
  showToast,
  providerTokens,
  setProviderTokens,
  providerBusy,
  tokenStatusLabel,
  onSaveCredential,
  onClearCredential,
  onTestCredential,
  onUpdateSelection,
  onRefreshSettings,
  dlCatalog,
  setDlCatalog,
  dlJobs,
  setDlJobs,
  confirmItem,
  setConfirmItem,
  dlBusy,
  setDlBusy,
}: {
  modality: ModalityTabKey;
  settings: SpeechKitSettingsState;
  showToast: (msg: string) => void;
  providerTokens: Record<string, string>;
  setProviderTokens: React.Dispatch<
    React.SetStateAction<Record<string, string>>
  >;
  providerBusy: Record<string, boolean>;
  tokenStatusLabel: (cred: ProviderCredentialState) => string;
  onSaveCredential: (provider: string) => void;
  onClearCredential: (provider: string) => void;
  onTestCredential: (provider: string) => void;
  onUpdateSelection: (
    mode: keyof SpeechKitSettingsState["modelSelections"],
    field: keyof ModeModelSelectionState,
    value: string,
  ) => void;
  onRefreshSettings: () => Promise<void>;
  dlCatalog: DownloadItem[];
  setDlCatalog: React.Dispatch<React.SetStateAction<DownloadItem[]>>;
  dlJobs: DownloadJob[];
  setDlJobs: React.Dispatch<React.SetStateAction<DownloadJob[]>>;
  confirmItem: DownloadItem | null;
  setConfirmItem: React.Dispatch<React.SetStateAction<DownloadItem | null>>;
  dlBusy: boolean;
  setDlBusy: React.Dispatch<React.SetStateAction<boolean>>;
}) {
  const profiles = settings.profiles ?? [];
  const filtered = profiles.filter((p) => p.modality === modality);
  const activeId = settings.activeProfiles?.[modality];
  const selectionMode = MODE_SELECTION_KEYS[modality];
  const currentSelection = settings.modelSelections[selectionMode];
  const visibleProfiles = limitProfilesToVisibleOptions(
    filtered,
    [
      currentSelection.primaryProfileId,
      currentSelection.fallbackProfileId,
      activeId ?? "",
    ].filter(Boolean),
    4,
  );

  return (
    <>
      <div className="overflow-hidden rounded-[24px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)]">
        {/* Panel header */}
        <div className="flex items-center gap-3 border-b border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-2)] px-4 py-2.5">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
            Model setup
          </span>
          <span className="text-[11px] text-[color:var(--sk-text-subtle)]">
            {modality === "stt"
              ? "Speech-to-Text"
              : modality === "assist"
                ? "Assist LLM"
                : "Voice Agent"}
          </span>
        </div>

        {visibleProfiles.length === 0 ? (
          <p className="px-4 py-4 text-[11px] text-[color:var(--sk-text-muted)]">
            No live-switchable model profiles are exposed in this build.
          </p>
        ) : (
          <div className="divide-y divide-[color:var(--sk-shell-divider)]">
            {visibleProfiles.map((profile) => {
              const isActive = activeId === profile.id;
              const isPrimarySelected =
                currentSelection.primaryProfileId === profile.id;
              const isFallbackSelected =
                currentSelection.fallbackProfileId === profile.id;
              const badge = sourceBadge(profile);
              const providerKey = profile.executionMode
                ? PROVIDER_FOR_EXECUTION_MODE[profile.executionMode]
                : undefined;
              const providerCredential = providerKey
                ? settings.providerCredentials?.[providerKey]
                : undefined;
              const providerMissing = Boolean(
                providerKey && !providerCredential?.available,
              );
              const providerCopy = providerCredential
                ? providerCredentialCopy(profile.name, providerCredential)
                : null;
              const providerReady = Boolean(
                providerKey && providerCredential?.available,
              );
              const providerIsBusy = providerKey
                ? (providerBusy[providerKey] ?? false)
                : false;
              const downloadItems = dlCatalog.filter(
                (item) => item.profileId === profile.id,
              );
              const localRuntimeProblem = downloadItems.find(
                (item) => item.kind === "http" && item.runtimeReady === false,
              )?.runtimeProblem;
              const localRuntimeMissing = Boolean(localRuntimeProblem);
              const downloadActive = downloadItems.some((item) => {
                const job = dlJobs.find(
                  (candidate) => candidate.modelId === item.id,
                );
                return job?.status === "pending" || job?.status === "running";
              });
              const downloadReady = downloadItems.some((item) => {
                const job = dlJobs.find(
                  (candidate) => candidate.modelId === item.id,
                );
                return item.available || job?.status === "done";
              });
              const needsDownload =
                downloadItems.length > 0 && !downloadReady && !downloadActive;
              const readyToUse =
                !providerMissing &&
                !needsDownload &&
                !downloadActive &&
                !localRuntimeMissing;
              const statusLabel = isPrimarySelected
                ? "Primary"
                : isFallbackSelected
                  ? "Fallback"
                  : providerMissing
                    ? (providerCopy?.neededLabel ??
                      `${providerCredential?.label ?? "API"} key needed`)
                    : localRuntimeMissing
                      ? "Runtime missing"
                      : downloadActive
                        ? "Downloading"
                        : needsDownload
                          ? "Download required"
                          : "Ready";
              const statusClassName =
                isPrimarySelected || isFallbackSelected || isActive
                  ? "border-[color:var(--sk-accent)]/25 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]"
                  : localRuntimeMissing
                    ? "border-amber-500/25 bg-amber-500/10 text-amber-200/80"
                    : "border-[color:var(--sk-border)] bg-transparent text-[color:var(--sk-text-muted)]";

              return (
                <div
                  key={profile.id}
                  className={
                    isPrimarySelected || isFallbackSelected || isActive
                      ? "bg-[color:var(--sk-accent)]/4"
                      : undefined
                  }
                >
                  {/* Main identity row */}
                  <div className="flex items-center gap-3 px-4 py-3">
                    <div
                      className={[
                        "size-2 shrink-0 rounded-full",
                        isPrimarySelected || isFallbackSelected || isActive
                          ? "bg-[color:var(--sk-accent)]"
                          : "bg-[color:var(--sk-border)]",
                      ].join(" ")}
                    />
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
                      <span
                        className={[
                          "text-[13px] font-medium",
                          isPrimarySelected || isFallbackSelected || isActive
                            ? "text-[color:var(--sk-accent)]"
                            : "text-[color:var(--sk-text)]/85",
                        ].join(" ")}
                      >
                        {profile.name}
                      </span>
                      <span
                        className={[
                          "shrink-0 rounded px-1.5 py-px text-[9px]",
                          badge.className,
                        ].join(" ")}
                      >
                        {badge.label}
                      </span>
                      <span className="text-[10px] text-[color:var(--sk-text-muted)]/70">
                        {profile.source ?? profile.executionMode ?? "local"}
                      </span>
                      {profile.description && (
                        <span className="truncate text-[11px] text-[color:var(--sk-text-muted)]/80">
                          {profile.description}
                        </span>
                      )}
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <span
                        className={[
                          "rounded-full border px-2 py-0.5 text-[10px]",
                          statusClassName,
                        ].join(" ")}
                      >
                        {statusLabel}
                      </span>
                      {isPrimarySelected ? (
                        <span className="w-24 text-right text-[11px] font-medium text-[color:var(--sk-accent)]/80">
                          Primary model
                        </span>
                      ) : isFallbackSelected ? (
                        <span className="w-24 text-right text-[11px] font-medium text-[color:var(--sk-accent)]/80">
                          Fallback model
                        </span>
                      ) : readyToUse ? (
                        <span className="w-24 text-right text-[10px] text-[color:var(--sk-text-muted)]">
                          Selectable below
                        </span>
                      ) : (
                        <span className="w-24 text-right text-[10px] text-[color:var(--sk-text-muted)]">
                          {providerMissing
                            ? (providerCopy?.unlockLabel ??
                              "Add the key above.")
                            : localRuntimeMissing
                              ? "Runtime missing."
                              : needsDownload
                                ? "Download required."
                                : "Downloading…"}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Provider missing: inline key entry */}
                  {providerMissing && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-amber-500/10 bg-amber-500/4 px-4 py-2">
                      <span className="shrink-0 text-[10px] font-medium text-amber-200/60">
                        {providerCopy?.title ??
                          `Add ${providerCredential.label} key`}
                      </span>
                      <input
                        aria-label={
                          providerCopy?.inputLabel ?? `${profile.name} API key`
                        }
                        type="password"
                        value={providerTokens[providerKey] ?? ""}
                        onChange={(e) =>
                          setProviderTokens((tokens) => ({
                            ...tokens,
                            [providerKey]: e.target.value,
                          }))
                        }
                        placeholder={
                          providerCopy?.placeholder ??
                          (providerCredential.envName || "API key")
                        }
                        className="h-7 flex-1 rounded border border-amber-500/15 bg-black/20 px-2.5 text-[11px] text-[#e4e1e9]/80 outline-none focus:border-[#947dff]/50"
                      />
                      <button
                        type="button"
                        onClick={() => onSaveCredential(providerKey)}
                        disabled={providerIsBusy}
                        className={[
                          "shrink-0 rounded border px-3 py-1 text-[11px] font-medium",
                          providerIsBusy
                            ? "border-[#484555] bg-[#35343a] text-[#938ea1]"
                            : "border-[#947dff]/25 bg-[#947dff]/15 text-[#cabeff] hover:bg-[#947dff]/25",
                        ].join(" ")}
                      >
                        {providerCopy?.saveLabel ?? "Save key"}
                      </button>
                    </div>
                  )}

                  {/* Provider ready: token management row */}
                  {providerReady && providerKey && providerCredential && (
                    <div className="flex items-center gap-2 border-t border-[color:var(--sk-shell-divider)] bg-[color:var(--sk-surface-0)]/65 px-4 py-2">
                      <span className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)]">
                        {tokenStatusLabel(providerCredential)}
                      </span>
                      <input
                        type="password"
                        aria-label={`Update ${providerCredential.label} ${providerSecretNoun(providerCredential.provider)}`}
                        placeholder={`Update ${providerSecretNoun(providerCredential.provider)}…`}
                        value={providerTokens[providerKey] ?? ""}
                        onChange={(e) =>
                          setProviderTokens((tokens) => ({
                            ...tokens,
                            [providerKey]: e.target.value,
                          }))
                        }
                        className="sk-input h-6 min-w-0 flex-1 rounded px-2 text-[11px]"
                      />
                      {(providerTokens[providerKey] ?? "").trim().length >
                        0 && (
                        <button
                          type="button"
                          onClick={() => onSaveCredential(providerKey)}
                          disabled={providerIsBusy}
                          className="shrink-0 rounded px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]/80 hover:text-[color:var(--sk-accent)]"
                        >
                          Save
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => onTestCredential(providerKey)}
                        disabled={providerIsBusy}
                        className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-[color:var(--sk-text)]"
                      >
                        Test
                      </button>
                      {providerCredential.hasStoredSecret && (
                        <button
                          type="button"
                          onClick={() => onClearCredential(providerKey)}
                          disabled={providerIsBusy}
                          className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-red-300/75"
                        >
                          Clear
                        </button>
                      )}
                    </div>
                  )}

                  {localRuntimeMissing && (
                    <div className="flex items-center gap-2 border-t border-amber-500/10 bg-amber-500/4 px-4 py-2">
                      <span className="text-[10px] leading-relaxed text-amber-200/75">
                        {localRuntimeProblem}
                      </span>
                    </div>
                  )}

                  {/* Download items */}
                  {downloadItems.length > 0 && (
                    <div className="space-y-1.5 border-t border-[color:var(--sk-shell-divider)] px-4 py-2">
                      {downloadItems.map((item) => {
                        const itemJob = dlJobs.find(
                          (candidate) => candidate.modelId === item.id,
                        );
                        const itemDownloadActive =
                          itemJob?.status === "pending" ||
                          itemJob?.status === "running";
                        const itemDownloadReady = Boolean(
                          item.available || itemJob?.status === "done",
                        );
                        const itemSelected = Boolean(item.selected);
                        const itemRuntimeMissing =
                          itemDownloadReady && item.runtimeReady === false;

                        return (
                          <div
                            key={item.id}
                            className="flex flex-wrap items-center gap-2"
                          >
                            <span className="text-[11px] text-[color:var(--sk-text)]">
                              {item.name}
                            </span>
                            {item.recommended && (
                              <span className="rounded bg-[color:var(--sk-accent-soft)] px-1.5 py-px text-[9px] text-[color:var(--sk-accent)]/80">
                                recommended
                              </span>
                            )}
                            <span className="text-[10px] text-[color:var(--sk-text-muted)]">
                              {item.sizeLabel}
                            </span>
                            {itemDownloadActive ? (
                              <>
                                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[color:var(--sk-surface-2)]">
                                  <div
                                    className="h-full rounded-full bg-[color:var(--sk-accent)]/70 transition-all duration-500"
                                    style={{
                                      width: `${Math.round((itemJob?.progress ?? 0) * 100)}%`,
                                    }}
                                  />
                                </div>
                                <span className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)]">
                                  {itemJob?.statusText}
                                </span>
                                <button
                                  type="button"
                                  onClick={() => {
                                    if (itemJob)
                                      cancelModelDownload(itemJob.id).catch(
                                        () => {},
                                      );
                                  }}
                                  className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)] hover:text-red-300/75"
                                >
                                  Cancel download
                                </button>
                              </>
                            ) : itemDownloadReady ? (
                              itemSelected ? (
                                <span className="text-[10px] text-emerald-300/80">
                                  Selected on this device
                                </span>
                              ) : itemRuntimeMissing ? (
                                <>
                                  <span className="text-[10px] text-amber-300/80">
                                    Model ready on this device
                                  </span>
                                  <span className="text-[10px] text-amber-200/70">
                                    {item.runtimeProblem ??
                                      "Local runtime missing."}
                                  </span>
                                </>
                              ) : (
                                <>
                                  <span className="text-[10px] text-emerald-300/80">
                                    Ready on this device
                                  </span>
                                  <button
                                    type="button"
                                    aria-label={`Use ${item.name}`}
                                    onClick={async () => {
                                      try {
                                        const result =
                                          await selectDownloadedModel(item.id);
                                        const [, freshCatalog] =
                                          await Promise.all([
                                            onRefreshSettings(),
                                            fetchDownloadCatalog(),
                                          ]);
                                        setDlCatalog(freshCatalog);
                                        showToast(
                                          result.message ??
                                            `${item.name} selected`,
                                        );
                                      } catch (error) {
                                        showToast(
                                          error instanceof Error
                                            ? error.message
                                            : "Switch failed",
                                        );
                                      }
                                    }}
                                    className="rounded border border-[color:var(--sk-border)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-accent)]"
                                  >
                                    Use model
                                  </button>
                                </>
                              )
                            ) : (
                              <button
                                type="button"
                                onClick={() => setConfirmItem(item)}
                                className="rounded border border-[color:var(--sk-border)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-accent)]"
                              >
                                Download
                              </button>
                            )}
                            {itemJob?.status === "failed" && (
                              <span className="ml-1 text-[10px] text-red-400/70">
                                {itemJob.error ?? "Download failed"}
                              </span>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {filtered.length > 0 ? (
        <div className="mt-4 rounded-[24px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] px-4 py-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
                Model routing
              </p>
              <p className="mt-1 text-[11px] text-[color:var(--sk-text-muted)]">
                Choose a primary model and one fallback for{" "}
                {modality === "stt"
                  ? "Transcribe"
                  : modality === "assist"
                    ? "Assist"
                    : "Voice Agent"}
                .
              </p>
            </div>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <SelectionField
              label="Primary model"
              value={currentSelection.primaryProfileId}
              options={filtered}
              onChange={(value) =>
                onUpdateSelection(selectionMode, "primaryProfileId", value)
              }
            />
            <SelectionField
              label="Fallback model"
              value={currentSelection.fallbackProfileId}
              options={filtered}
              onChange={(value) =>
                onUpdateSelection(selectionMode, "fallbackProfileId", value)
              }
              allowEmptyLabel="No fallback"
            />
          </div>
        </div>
      ) : null}

      {/* Download confirmation modal */}
      {confirmItem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
          <div className="w-80 rounded-xl border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] p-5 shadow-2xl">
            <h3 className="text-sm font-semibold text-[color:var(--sk-text)]">
              Download Model
            </h3>
            <p className="mt-2 text-xs font-medium text-[color:var(--sk-text)]/80">
              {confirmItem.name}
            </p>
            <p className="mt-1 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
              {confirmItem.description}
            </p>
            <div className="mt-3 flex flex-wrap gap-x-2 gap-y-0.5 text-[10px] text-[color:var(--sk-text-muted)]">
              <span>{confirmItem.sizeLabel}</span>
              <span>·</span>
              <span>License: {confirmItem.license}</span>
              {confirmItem.kind === "ollama" && (
                <>
                  <span>·</span>
                  <span className="text-amber-300/50">
                    Requires Ollama running locally
                  </span>
                </>
              )}
            </div>
            <div className="mt-4 flex gap-2">
              <button
                type="button"
                disabled={dlBusy}
                onClick={async () => {
                  setDlBusy(true);
                  try {
                    const job = await startModelDownload(confirmItem.id);
                    setDlJobs((prev) => [
                      ...prev.filter((j) => j.modelId !== confirmItem.id),
                      job,
                    ]);
                    setConfirmItem(null);
                  } catch (e) {
                    showToast(
                      e instanceof Error ? e.message : "Download failed",
                    );
                  } finally {
                    setDlBusy(false);
                  }
                }}
                className="flex-1 rounded-lg bg-[color:var(--sk-accent-soft)] py-1.5 text-xs font-medium text-[color:var(--sk-accent)] hover:bg-[color:var(--sk-accent)]/24 disabled:opacity-50"
              >
                {dlBusy ? "Starting…" : "Download"}
              </button>
              <button
                type="button"
                onClick={() => setConfirmItem(null)}
                className="flex-1 rounded-lg border border-[color:var(--sk-border)] py-1.5 text-xs text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-text)]"
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

function SelectionField({
  label,
  value,
  options,
  onChange,
  allowEmptyLabel = "None",
}: {
  label: string;
  value: string;
  options: NonNullable<SpeechKitSettingsState["profiles"]>;
  onChange: (value: string) => void;
  allowEmptyLabel?: string;
}) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-[11px] font-medium text-[color:var(--sk-text)]">
        {label}
      </span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="sk-input h-10 rounded-xl px-3 text-sm"
      >
        <option value="">{allowEmptyLabel}</option>
        {options.map((profile) => (
          <option key={profile.id} value={profile.id}>
            {profile.name}
          </option>
        ))}
      </select>
    </label>
  );
}

function Chip({
  active,
  ariaLabel,
  onClick,
  disabled = false,
  children,
  className = "",
}: {
  active: boolean;
  ariaLabel?: string;
  onClick?: () => void;
  disabled?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
      className={[
        "h-8 rounded-lg border px-3 text-xs font-medium transition-all",
        active
          ? "border-[color:var(--sk-accent)]/60 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]"
          : disabled
            ? "cursor-not-allowed border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] text-[color:var(--sk-text-subtle)]"
            : "border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-text)]",
        className,
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function Row({
  label,
  on,
  onToggle,
}: {
  label: string;
  on: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-sm text-[color:var(--sk-text)]">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={on}
        onClick={onToggle}
        className={[
          "relative inline-flex h-5.5 w-9.5 shrink-0 cursor-pointer items-center rounded-full transition-colors",
          on ? "bg-[color:var(--sk-accent)]" : "bg-[color:var(--sk-border)]",
        ].join(" ")}
      >
        <span
          className={[
            "inline-block h-4 w-4 rounded-full bg-white shadow transition-transform",
            on ? "translate-x-4.75" : "translate-x-0.75",
          ].join(" ")}
        />
      </button>
    </div>
  );
}

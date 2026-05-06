import { useCallback, useEffect, useRef, useState } from "react";
import { Dialogs } from "@wailsio/runtime";

import { providerSecretNoun } from "@/components/settings/secrets-panel";
import {
  MODE_DEFAULT_BASES,
  MODE_HOTKEY_BEHAVIOR_FIELDS,
  MODE_HOTKEY_FIELDS,
  MODE_SELECTION_KEYS,
  directoryFromPath,
  joinFolderAndFile,
  reconcileSettingsState,
  sqliteFilenameFromPath,
  type ConfigurableMode,
} from "@/components/settings/settings-state";
import {
  clearProviderCredential,
  defaultSettingsState,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  fetchModelProfiles,
  fetchOverlayState,
  fetchSettingsState,
  resetOverlayPosition,
  saveProviderCredential,
  saveSettingsState,
  testProviderCredential,
  updateProviderIntegration,
  type DownloadItem,
  type DownloadJob,
  type HotkeyBehavior,
  type ModeModelSelectionState,
  type ProviderCredentialState,
  type SpeechKitSettingsState,
} from "@/lib/speechkit";

type GeneralTab = "general" | "integrations" | "speechkit_server" | "storage";
type ModeTab = keyof typeof MODE_SELECTION_KEYS;
export type SettingsTab = GeneralTab | ModeTab;
type Tab = SettingsTab;

type UseSettingsControllerOptions = {
  initialTab?: SettingsTab;
  onToast?: (message: string) => void;
};

export function useSettingsController({
  initialTab = "general",
  onToast,
}: UseSettingsControllerOptions = {}) {
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
    onToast?.(message);
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
        [mode]: trimmedValue.length > 0,
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
    const patch: Partial<SpeechKitSettingsState> = {};
    patch[MODE_HOTKEY_BEHAVIOR_FIELDS[mode]] = value;
    updateSettings(patch);
  };

  const updateModelSelection = (
    mode: keyof SpeechKitSettingsState["modelSelections"],
    next: ModeModelSelectionState,
  ) => {
    updateSettings({
      modelSelections: {
        ...settings.modelSelections,
        [mode]: next,
      },
    });
  };

  const toggleModeEnabled = (mode: ConfigurableMode) => {
    const field = MODE_HOTKEY_FIELDS[mode];
    const currentValue = settings[field].trim();
    const nextEnabled = !settings.modeEnabled[mode];
    const fallbackHotkey = currentValue || MODE_DEFAULT_BASES[mode];

    const patch: Partial<SpeechKitSettingsState> = {
      modeEnabled: {
        ...settings.modeEnabled,
        [mode]: nextEnabled,
      },
    };
    patch[field] = fallbackHotkey;
    updateSettings(patch);
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

  const handleUpdateProviderIntegration = async (
    provider: string,
    enabled: boolean,
  ) => {
    setProviderBusy((b) => ({ ...b, [provider]: true }));
    try {
      const result = await updateProviderIntegration(provider, enabled);
      showToast(result.message ?? "Saved");
      await loadSettings();
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Integration update failed");
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

  const handleChooseStorageFolder = async (
    target: "sqlite" | "model_downloads",
  ) => {
    const currentPath =
      target === "sqlite" ? settings.sqlitePath : settings.modelDownloadDir;
    try {
      const folder = await Dialogs.OpenFile({
        Title:
          target === "sqlite"
            ? "Select SQLite storage folder"
            : "Select model download folder",
        ButtonText: "Use folder",
        CanChooseDirectories: true,
        CanChooseFiles: false,
        CanCreateDirectories: true,
        AllowsMultipleSelection: false,
        Directory:
          target === "sqlite" ? directoryFromPath(currentPath) : currentPath,
        Detached: true,
      });
      if (!folder) return;
      if (target === "sqlite") {
        updateSettings({
          sqlitePath: joinFolderAndFile(
            folder,
            sqliteFilenameFromPath(settings.sqlitePath),
          ),
        });
        return;
      }
      updateSettings({ modelDownloadDir: folder });
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Folder selection failed");
    }
  };

  return {
    settings,
    setSettings,
    providerTokens,
    setProviderTokens,
    providerBusy,
    loaded,
    toast,
    tab,
    setTab,
    dlCatalog,
    setDlCatalog,
    dlJobs,
    setDlJobs,
    confirmItem,
    setConfirmItem,
    dlBusy,
    setDlBusy,
    loadSettings,
    showToast,
    updateSettings,
    updateModeHotkey,
    updateModeHotkeyBehavior,
    updateModelSelection,
    toggleModeEnabled,
    tokenStatusLabel,
    postgresReady,
    handleSaveProviderCredential,
    handleClearProviderCredential,
    handleTestProviderCredential,
    handleUpdateProviderIntegration,
    handleSaveCurrentOverlaySpot,
    handleResetOverlaySpot,
    hasSavedOverlaySpot,
    handleChooseStorageFolder,
  };
}

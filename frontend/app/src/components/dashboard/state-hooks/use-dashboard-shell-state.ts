import { useCallback, useEffect, useRef, useState } from "react";

import type { SettingsTab } from "@/components/settings-app";
import { useDesktopTheme } from "@/lib/desktop-theme";
import { fetchAppVersion, fetchLogs, type AppVersionInfo } from "@/lib/speechkit";

import type { DashboardTab, DashboardToast } from "../dashboard-types";
import { useModelDownloadState } from "./use-model-download-state";

export const DASHBOARD_TAB_STORAGE_KEY = "speechkit.dashboard.tab";

export const dashboardTabMeta: Record<
  DashboardTab,
  { title: string; subtitle: string }
> = {
  dashboard: {
    title: "Dashboard",
    subtitle: "Your daily capture surface",
  },
  library: {
    title: "Library",
    subtitle: "Transcriptions, notes, voice sessions",
  },
  settings: {
    title: "Settings",
    subtitle: "Hotkeys, models, audio, storage",
  },
  logs: {
    title: "Logs",
    subtitle: "Application events and diagnostics",
  },
};

export function useDashboardShellState() {
  const [tab, setTab] = useState<DashboardTab>(() => resolveInitialDashboardTab());
  const { theme, toggleTheme } = useDesktopTheme("dark");
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("general");
  const [appVersionInfo, setAppVersionInfo] = useState<AppVersionInfo | null>(
    null,
  );
  const [showSetupWizard, setShowSetupWizard] = useState(false);
  const [setupChecked, setSetupChecked] = useState(false);
  const [toasts, setToasts] = useState<DashboardToast[]>([]);
  const toastIdRef = useRef(0);
  const modelDownloads = useModelDownloadState();

  useEffect(() => {
    let active = true;
    void fetch("/app/setup-status")
      .then((r) => r.json())
      .then((data) => {
        if (active) {
          setShowSetupWizard(!data.setupDone);
          setSetupChecked(true);
        }
      })
      .catch(() => {
        if (active) setSetupChecked(true);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    void fetchAppVersion()
      .then((next) => {
        if (active) {
          setAppVersionInfo(next);
        }
      })
      .catch(() => {
        if (active) {
          setAppVersionInfo(null);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const addToast = useCallback(
    (message: string, type: DashboardToast["type"] = "error") => {
      const id = ++toastIdRef.current;
      setToasts((prev) => [...prev.slice(-4), { id, message, type }]);
      setTimeout(
        () => setToasts((prev) => prev.filter((t) => t.id !== id)),
        5000,
      );
    },
    [],
  );

  const lastLogCountRef = useRef(0);
  useEffect(() => {
    if (!setupChecked || showSetupWizard) return;

    let active = true;
    let primed = false;
    void fetchLogs()
      .then((logs) => {
        if (active) {
          lastLogCountRef.current = logs.length;
          primed = true;
        }
      })
      .catch(() => {
        primed = true;
      });

    const interval = setInterval(async () => {
      if (!primed) return;
      try {
        const logs = await fetchLogs();
        if (logs.length > lastLogCountRef.current) {
          const newLogs = logs.slice(lastLogCountRef.current);
          for (const log of newLogs) {
            if (log.type === "error") addToast(log.message, "error");
          }
          lastLogCountRef.current = logs.length;
        }
      } catch {
        // Ignore transient log polling failures; the next interval retries.
      }
    }, 3000);
    return () => {
      active = false;
      clearInterval(interval);
    };
  }, [addToast, setupChecked, showSetupWizard]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    window.sessionStorage.setItem(DASHBOARD_TAB_STORAGE_KEY, tab);
    const nextURL = new URL(window.location.href);
    nextURL.hash = tab === "dashboard" ? "" : `#${tab}`;
    window.history.replaceState(
      {},
      "",
      `${nextURL.pathname}${nextURL.search}${nextURL.hash}`,
    );
  }, [tab]);

  return {
    tab,
    setTab,
    theme,
    toggleTheme,
    settingsTab,
    setSettingsTab,
    appVersionInfo,
    showSetupWizard,
    setShowSetupWizard,
    setupChecked,
    toasts,
    modelDownloads,
  };
}

function resolveInitialDashboardTab(): DashboardTab {
  if (typeof window === "undefined") return "dashboard";
  const hashTab = parseDashboardTab(window.location.hash);
  if (hashTab) return hashTab;
  const storedTab = parseDashboardTab(
    window.sessionStorage.getItem(DASHBOARD_TAB_STORAGE_KEY) ?? "",
  );
  if (storedTab) return storedTab;
  return "dashboard";
}

function parseDashboardTab(value: string): DashboardTab | null {
  const normalized = value.replace(/^#/, "").trim().toLowerCase();
  switch (normalized) {
    case "dashboard":
    case "library":
    case "settings":
    case "logs":
      return normalized;
    default:
      return null;
  }
}

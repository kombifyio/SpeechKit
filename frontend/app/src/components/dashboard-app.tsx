import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  LayoutGrid,
  LibraryBig,
  Settings2,
  TerminalSquare,
} from "lucide-react";
import { Window } from "@wailsio/runtime";

import { DesktopWindowFrame } from "@/components/desktop-window-frame";
import { SettingsApp, type SettingsTab } from "@/components/settings-app";
import { useDesktopTheme } from "@/lib/desktop-theme";
import {
  cancelModelDownload,
  cancelAppUpdateDownload,
  builtInPrimaryModelSelections,
  dashboardAudioDownloadURL,
  deleteQuickNote,
  defaultSettingsState,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  fetchAppUpdateJobs,
  fetchAppVersion,
  fetchAudioDevices,
  fetchDashboardStats,
  fetchHistory,
  fetchLogs,
  fetchQuickNotes,
  openAppUpdateInstaller,
  selectDownloadedModel,
  setAudioDevice,
  startModelDownload,
  startAppUpdateDownload,
  type AppUpdateJob,
  type AppVersionInfo,
  type AudioDevice,
  type DashboardStats,
  type DownloadItem,
  type DownloadJob,
  pinQuickNote,
  type LogEntry,
  type QuickNote,
  revealDashboardAudio,
  type TranscriptionRecord,
} from "@/lib/speechkit";

type Tab = "dashboard" | "library" | "settings" | "logs";

const DASHBOARD_TAB_STORAGE_KEY = "speechkit.dashboard.tab";
const dashboardTabMeta: Record<Tab, { title: string; subtitle: string }> = {
  dashboard: {
    title: "Dashboard",
    subtitle: "Your daily capture surface",
  },
  library: {
    title: "Library",
    subtitle: "Transcriptions and quick notes",
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

const onboardingHotkeys = {
  dictate: defaultSettingsState.dictateHotkey,
  assist: defaultSettingsState.assistHotkey,
  voiceAgent: defaultSettingsState.voiceAgentHotkey,
} as const;

const onboardingHotkeyLabels = {
  dictate: "Win+Alt",
  assist: "Ctrl+Win",
  voiceAgent: "Ctrl+Shift",
} as const;

export function DashboardApp() {
  const [tab, setTab] = useState<Tab>(() => resolveInitialDashboardTab());
  const { theme, toggleTheme } = useDesktopTheme("dark");
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("general");
  const [appVersionInfo, setAppVersionInfo] = useState<AppVersionInfo | null>(
    null,
  );
  const [showSetupWizard, setShowSetupWizard] = useState(false);
  const [setupChecked, setSetupChecked] = useState(false);
  const [toasts, setToasts] = useState<
    Array<{ id: number; message: string; type: "error" | "warn" | "success" }>
  >([]);
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
    (message: string, type: "error" | "warn" | "success" = "error") => {
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
    const interval = setInterval(async () => {
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
        /* ignore */
      }
    }, 3000);
    return () => clearInterval(interval);
  }, [addToast]);

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

  if (showSetupWizard && setupChecked) {
    return (
      <div className="desktop-shell-root h-screen w-screen">
        <SetupWizard
          catalog={modelDownloads.catalog}
          jobs={modelDownloads.jobs}
          onStartDownload={modelDownloads.startDownload}
          onCancelDownload={modelDownloads.cancelDownload}
          onSelectDownloadedModel={modelDownloads.selectModel}
          onComplete={(next) => {
            void fetch("/app/complete-setup", { method: "POST" });
            if (next?.settingsTab) {
              setSettingsTab(next.settingsTab);
            }
            if (next?.dashboardTab) {
              setTab(next.dashboardTab);
            }
            setShowSetupWizard(false);
          }}
        />
        <ModelDownloadDock
          catalog={modelDownloads.catalog}
          jobs={modelDownloads.jobs}
          onCancel={modelDownloads.cancelDownload}
        />
      </div>
    );
  }

  if (!setupChecked) {
    return (
      <div className="desktop-shell-root flex h-screen items-center justify-center text-sm text-[color:var(--sk-text-muted)]">
        Loading...
      </div>
    );
  }

  const currentTabMeta = dashboardTabMeta[tab];

  return (
    <DesktopWindowFrame
      appLabel="kombify SpeechKit"
      title={currentTabMeta.title}
      subtitle={currentTabMeta.subtitle}
      icon={<SpeechKitWindowIcon />}
      theme={theme}
      onToggleTheme={toggleTheme}
      sidebar={
        <DashboardSidebar
          tab={tab}
          appVersionInfo={appVersionInfo}
          onSelectTab={setTab}
        />
      }
      actions={
        <>
          <span className="hidden rounded-full border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-3 py-1 text-[10px] font-semibold uppercase tracking-[0.18em] text-[color:var(--sk-text-subtle)] md:inline-flex">
            {formatAppVersionLabel(appVersionInfo?.version)}
          </span>
          {(tab === "dashboard" || tab === "library") && (
            <button
              type="button"
              onClick={() =>
                void fetch("/quicknotes/open-capture", { method: "POST" })
              }
              className="sk-secondary-button rounded-full px-4 py-2 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
            >
              Quick Note
            </button>
          )}
        </>
      }
      contentClassName="bg-[color:var(--sk-surface-1)]/90"
      onClose={() => Window.Hide()}
    >
      <main className="flex min-h-0 flex-1 flex-col overflow-hidden text-[13px] text-[color:var(--sk-text)]">
        <div className="min-h-0 flex-1 overflow-hidden">
          {tab === "dashboard" && (
            <DashboardView
              appVersionInfo={appVersionInfo}
              onOpenLibrary={() => setTab("library")}
              onOpenSettings={() => setTab("settings")}
            />
          )}
          {tab === "library" && <LibraryView />}
          {tab === "settings" && (
            <div className="h-full min-h-0">
              <SettingsApp initialTab={settingsTab} />
            </div>
          )}
          {tab === "logs" && <LogsView />}
        </div>
      </main>

      {toasts.length > 0 && (
        <div className="fixed bottom-6 right-6 z-50 flex flex-col gap-2">
          {toasts.map((toast) => (
            <div
              key={toast.id}
              className={[
                "animate-in slide-in-from-right rounded-2xl border px-3 py-2 text-xs shadow-lg backdrop-blur-sm",
                toast.type === "error"
                  ? "border-red-400/25 bg-red-500/12 text-red-100"
                  : toast.type === "warn"
                    ? "border-amber-400/25 bg-amber-500/12 text-amber-100"
                    : "border-emerald-400/25 bg-emerald-500/12 text-emerald-100",
              ].join(" ")}
            >
              {toast.message}
            </div>
          ))}
        </div>
      )}

      <ModelDownloadDock
        catalog={modelDownloads.catalog}
        jobs={modelDownloads.jobs}
        onCancel={modelDownloads.cancelDownload}
      />
    </DesktopWindowFrame>
  );
}

/* ── Dashboard View ── */

function DashboardView({
  appVersionInfo,
  onOpenLibrary,
  onOpenSettings,
}: {
  appVersionInfo: AppVersionInfo | null;
  onOpenLibrary: () => void;
  onOpenSettings: () => void;
}) {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [history, setHistory] = useState<TranscriptionRecord[]>([]);
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([]);

  useEffect(() => {
    let active = true;
    void fetchDashboardStats()
      .then((next) => {
        if (active) setStats(next);
      })
      .catch(() => {
        if (active) setStats(null);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    void fetchHistory()
      .then((records) => {
        if (active) setHistory(records);
      })
      .catch(() => {
        if (active) setHistory([]);
      });
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes);
      })
      .catch(() => {
        if (active) setQuickNotes([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const sortedHistory = useMemo(
    () => sortByNewest(history, (r) => r.createdAt),
    [history],
  );
  const sortedQuickNotes = useMemo(
    () => sortByNewest(quickNotes, (n) => n.createdAt),
    [quickNotes],
  );
  const latestTranscription = sortedHistory[0] ?? null;
  const pinnedNotes = sortedQuickNotes.filter((n) => n.pinned);
  const featuredNotes =
    pinnedNotes.length > 0
      ? pinnedNotes.slice(0, 3)
      : sortedQuickNotes.slice(0, 3);

  return (
    <div
      data-testid="welcome-scroll"
      className="h-full overflow-y-auto px-8 py-8"
    >
      <div className="space-y-8 pb-12">
        <AppUpdateBanner appVersionInfo={appVersionInfo} />

        {/* KPI Row */}
        <div data-testid="welcome-kpis" className="grid grid-cols-4 gap-4">
          <KPICard
            label="Total Recordings"
            value={formatStatNumber(stats?.transcriptions)}
          />
          <KPICard
            label="Average WPM"
            value={formatAverageWPM(stats?.averageWordsPerMinute)}
          />
          <KPICard
            label="Total Words"
            value={formatStatNumber(stats?.totalWords)}
          />
          <KPICard
            label="Recorded Minutes"
            value={formatRecordedMinutes(stats?.totalAudioDurationMs)}
          />
        </div>

        {/* Recent activity: transcriptions + notes */}
        {latestTranscription || featuredNotes.length > 0 ? (
          <div>
            <h3 className="mb-4 text-xs font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
              Recent Activity
            </h3>
            <div className="grid gap-6 md:grid-cols-[1.4fr_1fr]">
              {/* Latest transcription */}
              <section className="sk-panel rounded-[24px] p-6">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                  Latest transcription
                </p>
                <h3 className="text-lg font-semibold text-[color:var(--sk-text)]">
                  Latest capture
                </h3>
                {latestTranscription ? (
                  <>
                    <p className="mt-4 text-sm leading-7 text-[color:var(--sk-text)]/85">
                      {latestTranscription.text}
                    </p>
                    <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-[color:var(--sk-text-muted)]">
                      <span>
                        {formatLibraryTimestamp(latestTranscription.createdAt)}
                      </span>
                      <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[color:var(--sk-accent)]">
                        {latestTranscription.provider}
                      </span>
                      {latestTranscription.model && (
                        <span className="rounded-full bg-[color:var(--sk-surface-0)] px-2 py-0.5 text-[color:var(--sk-text)]/80">
                          {formatTranscriptionModelLabel(
                            latestTranscription.model,
                          )}
                        </span>
                      )}
                    </div>
                  </>
                ) : (
                  <p className="mt-4 text-sm text-[color:var(--sk-text-muted)]">
                    No transcriptions yet.
                  </p>
                )}
              </section>

              {/* Quick notes */}
              <section className="sk-panel rounded-[24px] p-6">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                  Pinned notes
                </p>
                <h3 className="text-lg font-semibold text-[color:var(--sk-text)]">
                  Fast recall
                </h3>
                <div className="mt-4 flex flex-col gap-2">
                  {featuredNotes.map((note) => (
                    <div
                      key={note.id}
                      className="rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-4 py-3"
                    >
                      <p className="line-clamp-3 text-sm leading-6 text-[color:var(--sk-text)]/80">
                        {note.text}
                      </p>
                      <div className="mt-2 flex items-center gap-2 text-[10px] text-[color:var(--sk-text-muted)]">
                        <span>{formatLibraryTimestamp(note.createdAt)}</span>
                        {note.pinned && (
                          <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[color:var(--sk-accent)]">
                            Pinned
                          </span>
                        )}
                      </div>
                    </div>
                  ))}
                  {featuredNotes.length === 0 && (
                    <p className="text-sm text-[color:var(--sk-text-muted)]">
                      Create a quick note to keep names, snippets, or follow-ups
                      close.
                    </p>
                  )}
                </div>
              </section>
            </div>
          </div>
        ) : (
          /* Empty state / Quick Start */
          <div className="sk-panel rounded-[28px] p-8">
            <h3 className="text-xl font-semibold text-[color:var(--sk-text)]">
              Welcome to SpeechKit
            </h3>
            <p className="mt-2 max-w-[50ch] text-sm text-[color:var(--sk-text-muted)]">
              SpeechKit stays close to the edge of your screen, keeps quick
              notes nearby, and lets you move from a short thought to a full
              dictation without opening a heavy dashboard.
            </p>

            <div className="mt-7">
              <h4 className="text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                Quick Start
              </h4>
              <div className="mt-3 grid gap-3">
                <QuickStartCard number="01" title="Hold Windows Alt to talk">
                  Start dictation anywhere, keep speaking naturally, then
                  release when done.
                </QuickStartCard>
                <QuickStartCard number="02" title="Hover over the pill">
                  Create a quick note from the hover menu, or speak directly
                  into capture.
                </QuickStartCard>
                <QuickStartCard
                  number="03"
                  title="Say Summarize on selected text"
                >
                  Quick words trigger focused actions on the current selection.
                </QuickStartCard>
              </div>
            </div>

            <div className="mt-6 flex flex-wrap gap-2">
              <button
                type="button"
                onClick={onOpenLibrary}
                className="sk-primary-button rounded-full px-5 py-2 text-xs font-bold transition-all hover:opacity-90"
              >
                Open Library
              </button>
              <button
                type="button"
                onClick={onOpenSettings}
                className="sk-secondary-button rounded-full px-4 py-2 text-xs font-medium transition-colors"
              >
                Open Settings
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function AppUpdateBanner({
  appVersionInfo,
}: {
  appVersionInfo: AppVersionInfo | null;
}) {
  const latestVersion = appVersionInfo?.latestVersion;
  const updateURL = appVersionInfo?.updateURL;
  const downloadURL = appVersionInfo?.downloadURL;
  const [jobs, setJobs] = useState<AppUpdateJob[]>([]);
  const jobsRef = useRef<AppUpdateJob[]>([]);
  const [busyAction, setBusyAction] = useState<
    "download" | "cancel" | "open" | null
  >(null);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    jobsRef.current = jobs;
  }, [jobs]);

  useEffect(() => {
    setJobs([]);
    setActionError(null);
    if (!latestVersion) return;

    let active = true;
    const loadJobs = async () => {
      try {
        const next = await fetchAppUpdateJobs();
        if (!active) return;
        setJobs(next.filter((job) => job.version === latestVersion));
      } catch {
        if (active) {
          setJobs([]);
        }
      }
    };

    void loadJobs();

    const interval = window.setInterval(() => {
      const hasRunningJob = jobsRef.current.some(
        (job) => job.status === "pending" || job.status === "running",
      );
      if (hasRunningJob) {
        void loadJobs();
      }
    }, 1000);

    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [latestVersion]);

  if (!latestVersion) return null;

  const latestJob = pickLatestAppUpdateJob(jobs);
  const isRunning =
    latestJob?.status === "pending" || latestJob?.status === "running";
  const isDone = latestJob?.status === "done";
  const showDownload = !isRunning && !isDone && !!downloadURL;

  const handleDownload = async () => {
    if (!latestVersion) return;
    setBusyAction("download");
    setActionError(null);
    try {
      const job = await startAppUpdateDownload(latestVersion);
      setJobs((prev) => upsertAppUpdateJob(prev, job));
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Download failed",
      );
    } finally {
      setBusyAction(null);
    }
  };

  const handleCancel = async () => {
    if (!latestJob) return;
    setBusyAction("cancel");
    setActionError(null);
    try {
      await cancelAppUpdateDownload(latestJob.id);
      setJobs((prev) =>
        prev.map((job) =>
          job.id === latestJob.id
            ? { ...job, status: "cancelled", statusText: "Cancelled" }
            : job,
        ),
      );
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "Cancel failed");
    } finally {
      setBusyAction(null);
    }
  };

  const handleOpen = async () => {
    if (!latestJob) return;
    setBusyAction("open");
    setActionError(null);
    try {
      await openAppUpdateInstaller(latestJob.id);
    } catch (error) {
      setActionError(
        error instanceof Error ? error.message : "Installer launch failed",
      );
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="rounded-[22px] border border-[color:var(--sk-accent)]/18 bg-[color:var(--sk-accent-soft)] px-4 py-3 text-xs text-[color:var(--sk-accent)]">
      <div className="flex items-center gap-3">
        <span>Update available: v{latestVersion}</span>
        <div className="ml-auto flex items-center gap-2">
          {updateURL && (
            <a
              href={updateURL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-full border border-[color:var(--sk-accent)]/20 px-3 py-1 font-medium text-[color:var(--sk-accent)] transition-colors hover:bg-[color:var(--sk-accent)]/10"
            >
              Change log
            </a>
          )}
          {showDownload && (
            <button
              type="button"
              onClick={() => void handleDownload()}
              disabled={busyAction === "download"}
              className="rounded-full bg-[color:var(--sk-accent)]/16 px-3 py-1 font-medium text-[color:var(--sk-accent)] transition-colors hover:bg-[color:var(--sk-accent)]/24 disabled:cursor-not-allowed disabled:opacity-60"
            >
              Download
            </button>
          )}
          {isDone && (
            <button
              type="button"
              onClick={() => void handleOpen()}
              disabled={busyAction === "open"}
              className="rounded-full bg-[color:var(--sk-accent)]/16 px-3 py-1 font-medium text-[color:var(--sk-accent)] transition-colors hover:bg-[color:var(--sk-accent)]/24 disabled:cursor-not-allowed disabled:opacity-60"
            >
              Open installer
            </button>
          )}
        </div>
      </div>

      {(latestJob || actionError) && (
        <div className="mt-3 flex flex-col gap-2">
          {latestJob && (
            <>
              <div className="flex items-center gap-3 text-[11px] text-[color:var(--sk-text)]/80">
                <span className="truncate">{latestJob.assetName}</span>
                <span className="ml-auto">{latestJob.statusText}</span>
              </div>
              <div className="flex items-center gap-3">
                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[color:var(--sk-accent)]/10">
                  <div
                    className="h-full rounded-full bg-[color:var(--sk-accent)] transition-[width] duration-300"
                    style={{
                      width: `${Math.max(0, Math.min(100, latestJob.progress * 100))}%`,
                    }}
                  />
                </div>
                {isRunning && (
                  <button
                    type="button"
                    onClick={() => void handleCancel()}
                    disabled={busyAction === "cancel"}
                    className="rounded-full border border-[color:var(--sk-accent)]/20 px-3 py-1 text-[11px] font-medium text-[color:var(--sk-accent)] transition-colors hover:bg-[color:var(--sk-accent)]/10 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    Cancel download
                  </button>
                )}
              </div>
              {latestJob.error && (
                <p className="text-[11px] text-red-300">{latestJob.error}</p>
              )}
            </>
          )}
          {actionError && (
            <p className="text-[11px] text-red-300">{actionError}</p>
          )}
        </div>
      )}
    </div>
  );
}

function useModelDownloadState() {
  const [catalog, setCatalog] = useState<DownloadItem[]>([]);
  const [jobs, setJobs] = useState<DownloadJob[]>([]);

  const refreshCatalog = useCallback(async () => {
    const next = await fetchDownloadCatalog();
    setCatalog(next);
    return next;
  }, []);

  const refreshJobs = useCallback(async () => {
    const next = await fetchDownloadJobs();
    setJobs(next);
    return next;
  }, []);

  useEffect(() => {
    let active = true;

    void fetchDownloadCatalog()
      .then((next) => {
        if (active) {
          setCatalog(next);
        }
      })
      .catch(() => {});

    void fetchDownloadJobs()
      .then((next) => {
        if (active) {
          setJobs(next);
        }
      })
      .catch(() => {});

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const hasActiveJob = jobs.some(
      (job) => job.status === "pending" || job.status === "running",
    );
    if (!hasActiveJob) return;

    let active = true;
    const interval = window.setInterval(() => {
      void refreshJobs()
        .then((nextJobs) => {
          if (!active) return;
          const stillRunning = nextJobs.some(
            (job) => job.status === "pending" || job.status === "running",
          );
          if (!stillRunning) {
            void refreshCatalog().catch(() => {});
          }
        })
        .catch(() => {});
    }, 1000);

    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [jobs, refreshCatalog, refreshJobs]);

  const startDownload = useCallback(async (itemId: string) => {
    const job = await startModelDownload(itemId);
    setJobs((prev) => upsertModelDownloadJob(prev, job));
    return job;
  }, []);

  const cancelDownload = useCallback(async (jobId: string) => {
    await cancelModelDownload(jobId);
    setJobs((prev) =>
      prev.map((job) =>
        job.id === jobId
          ? { ...job, status: "cancelled", statusText: "Cancelled" }
          : job,
      ),
    );
  }, []);

  const selectModel = useCallback(
    async (itemId: string) => {
      const result = await selectDownloadedModel(itemId);
      await refreshCatalog();
      return result;
    },
    [refreshCatalog],
  );

  return {
    catalog,
    jobs,
    startDownload,
    cancelDownload,
    selectModel,
  };
}

/* ── Library View ── */

function LibraryView() {
  const [history, setHistory] = useState<TranscriptionRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [copiedId, setCopiedId] = useState<number | null>(null);
  const copyTimer = useRef<number | null>(null);
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([]);
  const [copiedNote, setCopiedNote] = useState<number | null>(null);
  const sortedHistory = useMemo(
    () => sortByNewest(history, (r) => r.createdAt),
    [history],
  );
  const sortedQuickNotes = useMemo(
    () => sortByNewest(quickNotes, (n) => n.createdAt),
    [quickNotes],
  );
  const pinnedQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => n.pinned),
    [sortedQuickNotes],
  );
  const recentQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => !n.pinned),
    [sortedQuickNotes],
  );

  useEffect(() => {
    let active = true;
    void fetchHistory()
      .then((records) => {
        if (!active) return;
        setHistory(records);
        setLoading(false);
      })
      .catch(() => {
        if (!active) return;
        setLoading(false);
      });
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes);
      })
      .catch(() => {});
    return () => {
      active = false;
      if (copyTimer.current) window.clearTimeout(copyTimer.current);
    };
  }, []);

  const copyText = useCallback((id: number, text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id);
      if (copyTimer.current) window.clearTimeout(copyTimer.current);
      copyTimer.current = window.setTimeout(() => setCopiedId(null), 1200);
    });
  }, []);

  const handlePinNote = async (id: number, pinned: boolean) => {
    try {
      await pinQuickNote(id, pinned);
      const notes = await fetchQuickNotes();
      setQuickNotes(notes);
    } catch {
      return;
    }
  };

  const handleDeleteNote = async (id: number) => {
    try {
      await deleteQuickNote(id);
      const notes = await fetchQuickNotes();
      setQuickNotes(notes);
    } catch {
      return;
    }
  };

  const handleCopyNote = (id: number, text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedNote(id);
    setTimeout(() => setCopiedNote(null), 1200);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Two-column layout */}
      <div className="flex min-h-0 flex-1 gap-4 px-8 py-8">
        {/* Left: Transcriptions */}
        <div className="flex min-h-0 flex-1 flex-col">
          <span className="mb-3 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
            Recent Transcriptions
          </span>
          <div className="sk-panel flex-1 overflow-y-auto rounded-[24px] p-1">
            {loading && (
              <p className="py-4 text-center text-xs text-[color:var(--sk-text-muted)]">
                Loading...
              </p>
            )}
            {!loading && sortedHistory.length === 0 && (
              <p className="py-8 text-center text-xs text-[color:var(--sk-text-muted)]">
                No transcriptions yet. Press your hotkey to start.
              </p>
            )}
            {!loading && sortedHistory.length > 0 && (
              <div className="flex flex-col gap-0.5">
                {sortedHistory.map((record) => (
                  <TranscriptionRow
                    key={record.id}
                    record={record}
                    copied={copiedId === record.id}
                    onCopy={copyText}
                    onRevealAudio={revealDashboardAudio}
                  />
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Right: Quick Notes */}
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex items-center justify-between mb-3">
            <span className="text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
              Quick Notes
            </span>
            <button
              type="button"
              onClick={() =>
                fetch("/quicknotes/open-editor", { method: "POST" })
              }
              className="sk-primary-button rounded-full px-4 py-1.5 text-xs font-bold transition-all hover:opacity-90"
            >
              + New
            </button>
          </div>
          <div className="sk-panel flex-1 overflow-y-auto rounded-[24px] p-3">
            {sortedQuickNotes.length === 0 && (
              <p className="py-4 text-center text-xs text-[color:var(--sk-text-muted)]">
                No quick notes yet.
              </p>
            )}
            <div className="flex flex-col gap-1.5">
              {pinnedQuickNotes.length > 0 && (
                <>
                  <span className="mb-1 mt-0.5 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-accent)]/80">
                    Pinned Notes
                  </span>
                  {pinnedQuickNotes.map((note) => (
                    <QuickNoteRow
                      key={note.id}
                      note={note}
                      copied={copiedNote === note.id}
                      onCopy={handleCopyNote}
                      onDelete={handleDeleteNote}
                      onPin={handlePinNote}
                      onRevealAudio={revealDashboardAudio}
                    />
                  ))}
                  {recentQuickNotes.length > 0 && (
                    <span className="mb-1 mt-2 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]/80">
                      Recent Notes
                    </span>
                  )}
                </>
              )}
              {(pinnedQuickNotes.length > 0
                ? recentQuickNotes
                : sortedQuickNotes
              ).map((note) => (
                <QuickNoteRow
                  key={note.id}
                  note={note}
                  copied={copiedNote === note.id}
                  onCopy={handleCopyNote}
                  onDelete={handleDeleteNote}
                  onPin={handlePinNote}
                  onRevealAudio={revealDashboardAudio}
                />
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Logs View ── */

function LogsView() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);

  const loadLogs = useCallback(async () => {
    try {
      return await fetchLogs();
    } catch {
      return null;
    }
  }, []);

  useEffect(() => {
    let active = true;
    const syncLogs = async () => {
      const entries = await loadLogs();
      if (!active) return;
      if (entries) setLogs(entries);
      setLoading(false);
    };
    void syncLogs();
    const timer = window.setInterval(() => void syncLogs(), 2000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [loadLogs]);

  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [logs]);

  return (
    <div className="flex h-full flex-col">
      <div
        ref={containerRef}
        className="flex-1 overflow-y-auto bg-[color:var(--sk-surface-0)] px-8 py-8 font-mono text-xs leading-relaxed text-[color:var(--sk-text)]"
      >
        <h2 className="mb-4 font-sans text-sm font-semibold text-[color:var(--sk-accent)]">
          Application Logs
        </h2>
        {loading && (
          <p className="text-[color:var(--sk-text-muted)]">Loading logs...</p>
        )}
        {!loading && logs.length === 0 && (
          <p className="text-[color:var(--sk-text-muted)]">No log entries.</p>
        )}
        {logs.map((entry, i) => (
          <div key={i} className="flex gap-2">
            <span className="shrink-0 text-[#938ea1]/50">
              {formatLogTime(entry.timestamp)}
            </span>
            <span className={logColor(entry.type)}>{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ── Setup Wizard ── */

type WizardStep = "welcome" | "provider" | "done";
type SetupWizardCompletion = {
  dashboardTab?: Tab;
  settingsTab?: SettingsTab;
};

function SetupWizard({
  catalog,
  jobs,
  onStartDownload,
  onCancelDownload,
  onSelectDownloadedModel,
  onComplete,
}: {
  catalog: DownloadItem[];
  jobs: DownloadJob[];
  onStartDownload: (itemId: string) => Promise<DownloadJob>;
  onCancelDownload: (jobId: string) => Promise<void>;
  onSelectDownloadedModel: (itemId: string) => Promise<{ message?: string }>;
  onComplete: (next?: SetupWizardCompletion) => void;
}) {
  const [step, setStep] = useState<WizardStep>("welcome");
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [selectedDevice, setSelectedDevice] = useState("");
  const [loading, setLoading] = useState(false);
  const [busyModelAction, setBusyModelAction] = useState<string | null>(null);
  const [modelActionError, setModelActionError] = useState<string | null>(null);
  const [preferredLocalModelId, setPreferredLocalModelId] = useState<
    string | null
  >(null);

  useEffect(() => {
    void fetchAudioDevices()
      .then((res) => {
        setDevices(res.devices);
        setSelectedDevice(
          res.selectedDeviceId || res.devices[0]?.deviceId || "",
        );
      })
      .catch(() => {});
  }, []);

  const handleDeviceSelect = (deviceId: string) => {
    setSelectedDevice(deviceId);
    void setAudioDevice(deviceId).catch(() => {});
  };

  const localCatalogItems = useMemo(
    () => catalog.filter((item) => item.profileId === "stt.local.whispercpp"),
    [catalog],
  );

  const localModelChoices = useMemo(() => {
    const onboardingIDs = ["whisper.ggml-large-v3-turbo", "whisper.ggml-small"];
    return onboardingIDs
      .map((id) => catalog.find((item) => item.id === id))
      .filter((item): item is DownloadItem => Boolean(item));
  }, [catalog]);

  useEffect(() => {
    if (preferredLocalModelId) {
      return;
    }
    const selectedLocalModel = localCatalogItems.find((item) => item.selected);
    if (selectedLocalModel) {
      setPreferredLocalModelId(selectedLocalModel.id);
    }
  }, [localCatalogItems, preferredLocalModelId]);

  const localModelIDs = useMemo(
    () => new Set(localModelChoices.map((item) => item.id)),
    [localModelChoices],
  );

  const chosenLocalModel = useMemo(
    () =>
      localCatalogItems.find((item) => item.id === preferredLocalModelId) ??
      null,
    [localCatalogItems, preferredLocalModelId],
  );

  const activeLocalJob = useMemo(
    () =>
      pickLatestModelDownloadJob(
        jobs.filter(
          (job) =>
            localModelIDs.has(job.modelId) &&
            (job.status === "pending" || job.status === "running"),
        ),
      ),
    [jobs, localModelIDs],
  );

  const continueLabel = activeLocalJob
    ? "Continue while model downloads"
    : "Continue";

  const handleModelDownload = async (item: DownloadItem) => {
    setBusyModelAction(`download:${item.id}`);
    setModelActionError(null);
    setPreferredLocalModelId(item.id);
    try {
      await onStartDownload(item.id);
    } catch (error) {
      setModelActionError(
        error instanceof Error ? error.message : "Download failed",
      );
    } finally {
      setBusyModelAction(null);
    }
  };

  const handleModelSelect = async (item: DownloadItem) => {
    setBusyModelAction(`select:${item.id}`);
    setModelActionError(null);
    setPreferredLocalModelId(item.id);
    try {
      await onSelectDownloadedModel(item.id);
    } catch (error) {
      setModelActionError(
        error instanceof Error ? error.message : "Model switch failed",
      );
    } finally {
      setBusyModelAction(null);
    }
  };

  const handleFinish = async () => {
    setLoading(true);
    try {
      setModelActionError(null);
      if (chosenLocalModel?.available && !chosenLocalModel.selected) {
        await onSelectDownloadedModel(chosenLocalModel.id);
      }
      const body = new URLSearchParams();
      body.set("dictate_hotkey", onboardingHotkeys.dictate);
      body.set("assist_hotkey", onboardingHotkeys.assist);
      body.set("voice_agent_hotkey", onboardingHotkeys.voiceAgent);
      body.set("audio_device_id", selectedDevice);
      body.set(
        "dictate_primary_profile_id",
        builtInPrimaryModelSelections.dictate.primaryProfileId,
      );
      body.set(
        "assist_primary_profile_id",
        builtInPrimaryModelSelections.assist.primaryProfileId,
      );
      body.set(
        "voice_primary_profile_id",
        builtInPrimaryModelSelections.voice_agent.primaryProfileId,
      );
      await fetch("/settings/update", { method: "POST", body });
    } catch (error) {
      setLoading(false);
      setModelActionError(
        error instanceof Error ? error.message : "Setup could not be completed",
      );
      return;
    }
    onComplete();
  };

  const handleChooseModel = (itemId: string) => {
    setPreferredLocalModelId(itemId);
    setModelActionError(null);
  };

  const handleSkipSetup = () => {
    setModelActionError(null);
    onComplete();
  };

  const handleCloudSetup = () => {
    setModelActionError(null);
    onComplete({
      dashboardTab: "settings",
      settingsTab: "stt",
    });
  };

  const STEPS: WizardStep[] = ["welcome", "provider", "done"];

  return (
    <div
      className={[
        "flex h-screen flex-col items-center bg-[#131318] text-[#e4e1e9] px-6 relative",
        step === "provider"
          ? "justify-start overflow-y-auto py-6"
          : "justify-center overflow-hidden",
      ].join(" ")}
    >
      {/* Ambient glow */}
      <div className="fixed top-[-10%] left-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />
      <div className="fixed bottom-[-10%] right-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />

      {step === "welcome" && (
        <div className="flex flex-col items-center text-center max-w-2xl w-full z-10">
          {/* Logo */}
          <div className="w-20 h-20 bg-[#2a292f] rounded-full flex items-center justify-center ambient-glow border border-white/5 mb-12">
            <svg
              className="w-10 h-10 text-[#947dff]"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5.91-3c-.49 0-.9.36-.98.85C16.52 14.2 14.47 16 12 16s-4.52-1.8-4.93-4.15a.998.998 0 0 0-.98-.85c-.61 0-1.09.54-1 1.14.49 3 2.89 5.35 5.91 5.78V20c0 .55.45 1 1 1s1-.45 1-1v-2.08a6.993 6.993 0 0 0 5.91-5.78c.1-.6-.39-1.14-1-1.14z" />
            </svg>
          </div>

          <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight text-[#e4e1e9]">
            Welcome to SpeechKit
          </h1>
          <p className="mt-4 text-lg text-[#b5b3c4] font-light leading-relaxed">
            Your voice, your workflow. Dictate, assist, and automate —
            hands-free.
          </p>

          {/* Feature cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full max-w-4xl mt-16">
            <FeatureCard
              icon="dictate"
              title="Dictation"
              desc="Speech-to-text anywhere"
            />
            <FeatureCard
              icon="sparkle"
              title="AI Assist"
              desc="Voice-powered AI responses"
            />
            <FeatureCard
              icon="wave"
              title="Voice Agent"
              desc="Real-time audio conversations"
            />
          </div>

          <div className="flex flex-col items-center space-y-4 mt-16">
            <button
              type="button"
              onClick={() => setStep("provider")}
              className="signature-gradient ambient-glow text-[#2b0088] font-bold text-lg px-12 h-14 rounded-full transition-all active:scale-95 hover:opacity-90"
            >
              Get Started
            </button>
            <button
              type="button"
              onClick={handleSkipSetup}
              className="text-[#b5b3c4] text-sm font-medium hover:text-[#e4e1e9] transition-colors"
            >
              Skip setup
            </button>
          </div>
        </div>
      )}

      {step === "provider" && (
        <div className="z-10 w-full max-w-128 space-y-5 pb-3">
          {/* Progress dots */}
          <div className="flex justify-center items-center gap-3">
            {STEPS.map((s, i) => (
              <div
                key={s}
                className={[
                  "w-2.5 h-2.5 rounded-full transition-all",
                  i <= STEPS.indexOf(step)
                    ? "bg-[#cabeff] shadow-[0_0_8px_rgba(202,190,255,0.4)]"
                    : "bg-[#484555]",
                ].join(" ")}
              />
            ))}
          </div>

          <div className="text-center space-y-2">
            <h2 className="text-2xl font-extrabold tracking-tight">
              Local Dictation Setup
            </h2>
            <p className="text-[#b5b3c4] text-[13px] max-w-xl mx-auto leading-6">
              Choose the local Whisper model that SpeechKit should download
              first. You can always switch models later in Settings.
            </p>
          </div>

          <div className="space-y-2.5">
            {localModelChoices.map((item) => {
              const itemJob = jobs.find((job) => job.modelId === item.id);
              const itemDownloading =
                itemJob?.status === "pending" || itemJob?.status === "running";
              const itemBusy =
                busyModelAction === `download:${item.id}` ||
                busyModelAction === `select:${item.id}`;
              return (
                <WizardLocalModelCard
                  key={item.id}
                  item={item}
                  job={itemJob}
                  busy={itemBusy}
                  selectedForSetup={
                    preferredLocalModelId === item.id ||
                    (!preferredLocalModelId && item.selected)
                  }
                  onDownload={() => void handleModelDownload(item)}
                  onCancel={() =>
                    itemJob ? void onCancelDownload(itemJob.id) : undefined
                  }
                  onChooseModel={() => handleChooseModel(item.id)}
                  onUseModel={() => void handleModelSelect(item)}
                  downloading={itemDownloading}
                />
              );
            })}
          </div>

          {modelActionError && (
            <div className="rounded-xl border border-red-400/15 bg-red-500/8 px-4 py-3 text-xs text-red-200/85">
              {modelActionError}
            </div>
          )}

          <div className="bg-[#1b1b20] rounded-xl px-4 py-3 flex items-start gap-3">
            <svg
              className="w-5 h-5 text-[#947dff] shrink-0 mt-0.5"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z" />
            </svg>
            <p className="text-[#938ea1] text-xs leading-relaxed">
              SpeechKit downloads local models in the background and
              automatically wires them into Whisper.cpp as soon as the transfer
              finishes. You can continue immediately and keep using the app
              while the overlay tracks progress.
            </p>
          </div>

          {/* Mic selection (compact) */}
          {devices.length > 0 && (
            <div className="space-y-2">
              <label className="text-xs font-bold text-[#938ea1] uppercase tracking-widest mb-2 block">
                Microphone
              </label>
              <select
                value={selectedDevice}
                onChange={(e) => handleDeviceSelect(e.target.value)}
                className="w-full bg-[#0e0e13] border-none rounded-lg px-4 py-2.5 text-sm text-[#e4e1e9] focus:ring-1 focus:ring-[#cabeff]/40 appearance-none"
              >
                {devices.map((d) => (
                  <option key={d.deviceId} value={d.deviceId}>
                    {d.label}
                    {d.isDefault ? " (Default)" : ""}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="sticky bottom-0 space-y-3 border-t border-[#35343a]/50 bg-[#131318]/95 pt-4 backdrop-blur-xl">
            <button
              type="button"
              onClick={() => setStep("done")}
              className={[
                "w-full h-12 rounded-full font-bold transition-all flex items-center justify-center gap-2",
                "signature-gradient text-[#2b0088] ambient-glow active:scale-[0.98]",
              ].join(" ")}
            >
              {continueLabel}
            </button>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <button
                type="button"
                onClick={() => setStep("welcome")}
                className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
              >
                Back
              </button>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={handleCloudSetup}
                  className="rounded-full border border-[#484555] px-3 py-1.5 text-[11px] font-medium text-[#cabeff] hover:border-[#cabeff]/40"
                >
                  Use Hugging Face token instead
                </button>
                <button
                  type="button"
                  onClick={handleCloudSetup}
                  className="rounded-full border border-[#484555] px-3 py-1.5 text-[11px] font-medium text-[#cabeff] hover:border-[#cabeff]/40"
                >
                  Use OpenAI key instead
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {step === "done" && (
        <div className="flex flex-col items-center text-center max-w-4xl w-full z-10">
          {/* Progress dots */}
          <div className="flex gap-3 mb-12">
            {STEPS.map((s) => (
              <div key={s} className="w-2.5 h-2.5 rounded-full bg-[#cabeff]" />
            ))}
          </div>

          {/* Success icon */}
          <div className="w-24 h-24 rounded-full signature-gradient flex items-center justify-center ambient-glow ring-8 ring-[#cabeff]/10 mb-8">
            <svg
              className="w-12 h-12 text-[#2b0088]"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2.5}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M5 13l4 4L19 7"
              />
            </svg>
          </div>

          <h2 className="text-4xl font-extrabold tracking-tight mb-4">
            You're all set!
          </h2>
          <p className="text-lg text-[#b5b3c4] max-w-md leading-relaxed">
            SpeechKit is ready. Here is how to get started:
          </p>

          {/* Quick-start cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full mt-12 mb-12">
            <WizardFeatureCard
              title={onboardingHotkeyLabels.dictate}
              desc="Start dictation anywhere"
            />
            <WizardFeatureCard
              title={onboardingHotkeyLabels.assist}
              desc="Activate AI Assist"
            />
            <WizardFeatureCard
              title={onboardingHotkeyLabels.voiceAgent}
              desc="Start Voice Agent"
            />
          </div>

          <button
            type="button"
            onClick={() => void handleFinish()}
            disabled={loading}
            className="signature-gradient text-[#2b0088] h-14 rounded-full font-bold text-lg px-12 ambient-glow hover:scale-[1.02] active:scale-[0.98] transition-all disabled:opacity-50"
          >
            {loading ? "Setting up..." : "Start Using SpeechKit"}
          </button>
        </div>
      )}
    </div>
  );
}

/* ── Shared Components ── */

function SpeechKitWindowIcon() {
  return (
    <svg
      className="h-5 w-5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      aria-hidden="true"
    >
      <path d="M5 8.5h2.5v7H5z" fill="currentColor" stroke="none" />
      <path d="M10.75 5.5h2.5v13h-2.5z" fill="currentColor" stroke="none" />
      <path d="M16.5 9.5H19v5h-2.5z" fill="currentColor" stroke="none" />
      <path d="M3 12h18" opacity="0.18" />
    </svg>
  );
}

function DashboardSidebar({
  tab,
  appVersionInfo,
  onSelectTab,
}: {
  tab: Tab;
  appVersionInfo: AppVersionInfo | null;
  onSelectTab: (tab: Tab) => void;
}) {
  return (
    <>
      <nav className="flex-1 space-y-1 px-3 py-4">
        <NavBtn
          active={tab === "dashboard"}
          onClick={() => onSelectTab("dashboard")}
        >
          <LayoutGrid className="h-4.5 w-4.5 shrink-0" />
          Dashboard
        </NavBtn>
        <NavBtn
          active={tab === "library"}
          onClick={() => onSelectTab("library")}
        >
          <LibraryBig className="h-4.5 w-4.5 shrink-0" />
          Library
        </NavBtn>
        <NavBtn
          active={tab === "settings"}
          onClick={() => onSelectTab("settings")}
        >
          <Settings2 className="h-4.5 w-4.5 shrink-0" />
          Settings
        </NavBtn>
        <NavBtn active={tab === "logs"} onClick={() => onSelectTab("logs")}>
          <TerminalSquare className="h-4.5 w-4.5 shrink-0" />
          Logs
        </NavBtn>
      </nav>

      <div className="px-3 pb-3">
        <div className="rounded-[22px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-4 py-3">
          <p className="sk-kicker">Version</p>
          <p className="mt-1 text-sm font-medium text-[color:var(--sk-text)]">
            {appVersionInfo?.version
              ? `Build ${formatAppVersionLabel(appVersionInfo.version)}`
              : "Custom chrome active"}
          </p>
          <p className="mt-2 text-xs leading-5 text-[color:var(--sk-text-muted)]">
            Light and dark chrome now share the same desktop shell and controls.
          </p>
        </div>
      </div>
    </>
  );
}

function NavBtn({
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
        "flex items-center gap-3 w-full px-3 py-2.5 rounded-2xl text-sm transition-all",
        active
          ? "bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)] font-semibold"
          : "text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="sk-panel rounded-[24px] p-5 transition-all hover:bg-[color:var(--sk-surface-2)]">
      <p className="mb-2 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
        {label}
      </p>
      <span className="text-2xl font-bold text-[color:var(--sk-text)]">
        {value}
      </span>
    </div>
  );
}

function FeatureCard({
  icon,
  title,
  desc,
}: {
  icon: string;
  title: string;
  desc: string;
}) {
  const iconPath =
    icon === "dictate"
      ? "M4 11h2v8H4zm4-4h2v12H8zm4-3h2v15h-2zm4 5h2v10h-2z"
      : icon === "sparkle"
        ? "M12 2L9.19 8.63 2 9.24l5.46 4.73L5.82 21 12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2z"
        : "M7 18h2V6H7v12zm4-12v12h2V6h-2zm-8 8h2v-4H3v4zm12-6v8h2V8h-2zm4 2v4h2v-4h-2z";
  return (
    <div className="sk-panel rounded-[24px] p-8 text-center transition-all group hover:bg-[color:var(--sk-surface-2)]">
      <div className="mb-6 flex h-12 w-12 items-center justify-center rounded-xl bg-[color:var(--sk-surface-0)] text-[color:var(--sk-accent)] transition-transform group-hover:scale-110">
        <svg className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor">
          <path d={iconPath} />
        </svg>
      </div>
      <h3 className="mb-2 text-xl font-bold text-[color:var(--sk-text)]">
        {title}
      </h3>
      <p className="text-sm text-[color:var(--sk-text-muted)]">{desc}</p>
    </div>
  );
}

function WizardLocalModelCard({
  item,
  job,
  busy,
  selectedForSetup,
  downloading,
  onDownload,
  onCancel,
  onChooseModel,
  onUseModel,
}: {
  item: DownloadItem;
  job?: DownloadJob;
  busy: boolean;
  selectedForSetup: boolean;
  downloading: boolean;
  onDownload: () => void;
  onCancel: () => void;
  onChooseModel: () => void;
  onUseModel: () => void;
}) {
  const selected = item.selected;
  const ready = item.available || job?.status === "done";
  const runtimeMissing = ready && item.runtimeReady === false;
  const readyToUse = ready && !runtimeMissing;
  const chooseButtonLabel = selectedForSetup
    ? "Chosen for setup"
    : readyToUse
      ? "Use after setup"
      : "Choose";
  const statusLabel = downloading
    ? "Downloading"
    : selected
      ? "Selected"
      : runtimeMissing
        ? "Runtime missing"
        : ready
          ? "Ready"
          : "Not downloaded";

  return (
    <div
      className={[
        "rounded-2xl border px-4 py-3.5 transition-all",
        selectedForSetup
          ? "border-[#cabeff]/35 bg-[#cabeff]/8"
          : "border-[#35343a] bg-[#1b1b20]",
      ].join(" ")}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[#e4e1e9] font-semibold text-base leading-6">
              {item.name}
            </h3>
            {item.recommended && (
              <span className="rounded-full bg-[#cabeff]/15 px-2 py-1 text-[10px] font-bold uppercase tracking-widest text-[#cabeff]">
                Recommended
              </span>
            )}
            {selectedForSetup && (
              <span className="rounded-full bg-emerald-500/10 px-2 py-1 text-[10px] font-bold uppercase tracking-widest text-emerald-200/85">
                Starter model
              </span>
            )}
          </div>
          <p className="mt-1 text-[13px] leading-6 text-[#b5b3c4]">
            {item.description}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-[10px] text-[#938ea1]">
            <span>{item.sizeLabel}</span>
            <span>License: {item.license}</span>
            {selected && (
              <span className="text-emerald-200/85">
                Selected on this device
              </span>
            )}
            {!selected && readyToUse && (
              <span className="text-emerald-200/85">Ready on this device</span>
            )}
            {runtimeMissing && (
              <span className="text-amber-200/85">
                {item.runtimeProblem ?? "Local runtime missing."}
              </span>
            )}
          </div>
          {downloading && job && (
            <div className="mt-3 space-y-1.5">
              <div className="h-1.5 overflow-hidden rounded-full bg-[#0e0e13]">
                <div
                  className="h-full rounded-full bg-[#cabeff] transition-all duration-500"
                  style={{
                    width: `${Math.max(6, Math.round(job.progress * 100))}%`,
                  }}
                />
              </div>
              <div className="text-[10px] text-[#938ea1]">{job.statusText}</div>
            </div>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-2">
          <div className="rounded-full border border-[#484555] px-3 py-1 text-[10px] text-[#938ea1]">
            {statusLabel}
          </div>
          {readyToUse ? (
            selected ? (
              <button
                type="button"
                disabled
                className="rounded-full border border-emerald-300/20 px-3 py-1.5 text-[11px] font-medium text-emerald-200/80 opacity-80"
              >
                Active on device
              </button>
            ) : (
              <button
                type="button"
                aria-label={`Use ${item.name} after setup`}
                onClick={onUseModel}
                disabled={busy}
                className={[
                  "rounded-full border px-3 py-1.5 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-70",
                  selectedForSetup
                    ? "border-emerald-300/20 bg-emerald-500/10 text-emerald-200/85"
                    : "border-[#484555] text-[#cabeff] hover:border-[#cabeff]/40",
                ].join(" ")}
              >
                {chooseButtonLabel}
              </button>
            )
          ) : (
            <>
              <button
                type="button"
                aria-label={`Choose ${item.name} for setup`}
                onClick={onChooseModel}
                disabled={busy || selectedForSetup}
                className={[
                  "rounded-full border px-3 py-1.5 text-[11px] font-medium disabled:cursor-not-allowed disabled:opacity-70",
                  selectedForSetup
                    ? "border-emerald-300/20 bg-emerald-500/10 text-emerald-200/85"
                    : "border-[#484555] text-[#cabeff] hover:border-[#cabeff]/40",
                ].join(" ")}
              >
                {chooseButtonLabel}
              </button>
              {downloading ? (
                <button
                  type="button"
                  onClick={onCancel}
                  className="rounded border border-[#484555] px-2.5 py-1.5 text-[10px] text-[#938ea1] hover:border-red-300/40 hover:text-red-200"
                >
                  Cancel
                </button>
              ) : (
                <button
                  type="button"
                  aria-label={`Download ${item.name}`}
                  onClick={onDownload}
                  disabled={busy}
                  className="rounded-full bg-[#cabeff]/20 px-3 py-1.5 text-[11px] font-bold text-[#cabeff] hover:bg-[#cabeff]/30 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  Download
                </button>
              )}
            </>
          )}
          {selected && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit uses this model now.
            </span>
          )}
          {!selected && readyToUse && selectedForSetup && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit will switch after setup.
            </span>
          )}
          {!readyToUse && selectedForSetup && (
            <span className="text-[10px] font-medium text-emerald-200/85">
              SpeechKit will prefer this model first.
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

function WizardFeatureCard({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="bg-[#1b1b20] p-6 rounded-xl hover:bg-[#2a292f] transition-all flex flex-col items-center text-center">
      <h3 className="text-[#e4e1e9] font-bold mb-1">{title}</h3>
      <p className="text-[#b5b3c4] text-sm">{desc}</p>
    </div>
  );
}

function ModelDownloadDock({
  catalog,
  jobs,
  onCancel,
}: {
  catalog: DownloadItem[];
  jobs: DownloadJob[];
  onCancel: (jobId: string) => Promise<void>;
}) {
  const activeJob = pickLatestModelDownloadJob(
    jobs.filter((job) => job.status === "pending" || job.status === "running"),
  );
  if (!activeJob) return null;

  const item = catalog.find((candidate) => candidate.id === activeJob.modelId);

  return (
    <div className="fixed bottom-4 left-4 z-50 w-80 rounded-2xl border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-window)]/95 p-4 shadow-2xl backdrop-blur-xl">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
            Model download
          </p>
          <p className="mt-1 truncate text-sm font-medium text-[color:var(--sk-text)]">
            {item?.name ?? activeJob.modelId}
          </p>
        </div>
        <button
          type="button"
          onClick={() => void onCancel(activeJob.id)}
          className="rounded border border-[color:var(--sk-border)] px-2 py-1 text-[10px] text-[color:var(--sk-text-muted)] hover:border-red-300/40 hover:text-red-300"
        >
          Cancel download
        </button>
      </div>

      <div className="mt-3 h-2 overflow-hidden rounded-full bg-[color:var(--sk-surface-0)]">
        <div
          className="h-full rounded-full bg-[color:var(--sk-accent)] transition-all duration-500"
          style={{
            width: `${Math.max(6, Math.round(activeJob.progress * 100))}%`,
          }}
        />
      </div>
      <div className="mt-2 flex items-center justify-between gap-3 text-[11px] text-[color:var(--sk-text-muted)]">
        <span>{activeJob.statusText}</span>
        <span>{Math.round(activeJob.progress * 100)}%</span>
      </div>
    </div>
  );
}

function QuickStartCard({
  number,
  title,
  children,
}: {
  number: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-start gap-3 rounded-[20px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-4 py-3">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[color:var(--sk-surface-2)] text-[color:var(--sk-accent)]">
        <span className="text-xs font-bold">{number}</span>
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-[color:var(--sk-text)]">
          {title}
        </p>
        <p className="mt-1 text-xs leading-6 text-[color:var(--sk-text-muted)]">
          {children}
        </p>
      </div>
    </div>
  );
}

function QuickNoteRow({
  note,
  copied,
  onCopy,
  onDelete,
  onPin,
  onRevealAudio,
}: {
  note: QuickNote;
  copied: boolean;
  onCopy: (id: number, text: string) => void;
  onDelete: (id: number) => void;
  onPin: (id: number, pinned: boolean) => Promise<void>;
  onRevealAudio: (
    kind: "transcription" | "quicknote",
    id: number,
  ) => Promise<string>;
}) {
  return (
    <div
      data-testid="quicknote-row"
      className="group rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2"
    >
      <p className="line-clamp-3 text-xs leading-relaxed text-[color:var(--sk-text)]/75">
        {note.text}
      </p>
      <div className="mt-1.5 flex items-center gap-2">
        <span className="text-[10px] text-[color:var(--sk-text-muted)]">
          {formatLibraryTimestamp(note.createdAt)}
        </span>
        {note.pinned && (
          <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]">
            Pinned
          </span>
        )}
        {note.provider && note.provider !== "manual" && (
          <span className="rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)]">
            {note.provider}
          </span>
        )}
        {note.audio && (
          <span className="rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
            {formatAudioDuration(note.audio.durationMs)}
          </span>
        )}
        <div className="ml-auto flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
          {note.audio && (
            <>
              <a
                href={dashboardAudioDownloadURL("quicknote", note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
                aria-label="Download audio"
              >
                Download audio
              </a>
              <button
                type="button"
                onClick={() => void onRevealAudio("quicknote", note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
                aria-label="Show file"
              >
                Show file
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => void onPin(note.id, !note.pinned)}
            className={`rounded px-1.5 py-0.5 text-[10px] ${
              note.pinned
                ? "text-[color:var(--sk-accent)] hover:bg-[color:var(--sk-accent-soft)]"
                : "text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
            }`}
          >
            {note.pinned ? "Unpin" : "Pin"}
          </button>
          <button
            type="button"
            onClick={() =>
              fetch(`/quicknotes/open-editor?id=${note.id}`, { method: "POST" })
            }
            className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-accent)]/70 hover:bg-[color:var(--sk-accent-soft)] hover:text-[color:var(--sk-accent)]"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => onCopy(note.id, note.text)}
            className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
          >
            {copied ? "Copied!" : "Copy"}
          </button>
          <button
            type="button"
            onClick={() => onDelete(note.id)}
            className="rounded px-1.5 py-0.5 text-[10px] text-red-400/60 hover:bg-red-500/10 hover:text-red-400"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

function TranscriptionRow({
  record,
  copied,
  onCopy,
  onRevealAudio,
}: {
  record: TranscriptionRecord;
  copied: boolean;
  onCopy: (id: number, text: string) => void;
  onRevealAudio: (
    kind: "transcription" | "quicknote",
    id: number,
  ) => Promise<string>;
}) {
  return (
    <div
      data-testid="transcription-row"
      className="group flex items-start gap-3 rounded-[18px] px-3 py-2.5 transition-colors hover:bg-[color:var(--sk-surface-2)]/70"
    >
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm leading-snug text-[color:var(--sk-text)]/82">
          {record.text}
        </p>
        <div className="mt-1 flex items-center gap-1.5 overflow-hidden">
          <span className="shrink-0 rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px] font-medium text-[color:var(--sk-text-muted)]">
            {record.provider}
          </span>
          {record.model && (
            <span className="shrink-0 truncate rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]">
              {formatTranscriptionModelLabel(record.model)}
            </span>
          )}
          {record.audio && (
            <span className="shrink-0 rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
              {formatAudioDuration(record.audio.durationMs)}
            </span>
          )}
          <span className="shrink-0 text-[11px] text-[color:var(--sk-text-muted)]">
            {formatLibraryTimestamp(record.createdAt)}
          </span>
        </div>
      </div>
      <div className="mt-0.5 flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {record.audio && (
          <>
            <a
              href={dashboardAudioDownloadURL("transcription", record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
              aria-label="Download audio"
            >
              Download audio
            </a>
            <button
              type="button"
              onClick={() => void onRevealAudio("transcription", record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
              aria-label="Show file"
            >
              Show file
            </button>
          </>
        )}
        <button
          type="button"
          onClick={() => onCopy(record.id, record.text)}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[color:var(--sk-text-muted)] transition-colors hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-accent)]"
          title="Copy to clipboard"
        >
          {copied ? (
            <span className="text-[10px] font-medium text-emerald-400">
              Copied!
            </span>
          ) : (
            <ClipboardIcon />
          )}
        </button>
      </div>
    </div>
  );
}

function ClipboardIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

/* ── Utilities ── */

function sortByNewest<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort(
    (a, b) => new Date(getDate(b)).getTime() - new Date(getDate(a)).getTime(),
  );
}

function pickLatestAppUpdateJob(jobs: AppUpdateJob[]): AppUpdateJob | null {
  return jobs[jobs.length - 1] ?? null;
}

function upsertAppUpdateJob(
  jobs: AppUpdateJob[],
  nextJob: AppUpdateJob,
): AppUpdateJob[] {
  const existingIndex = jobs.findIndex((job) => job.id === nextJob.id);
  if (existingIndex < 0) {
    return [...jobs, nextJob];
  }
  return jobs.map((job) => (job.id === nextJob.id ? nextJob : job));
}

function pickLatestModelDownloadJob(jobs: DownloadJob[]): DownloadJob | null {
  return jobs[jobs.length - 1] ?? null;
}

function upsertModelDownloadJob(
  jobs: DownloadJob[],
  nextJob: DownloadJob,
): DownloadJob[] {
  const existingIndex = jobs.findIndex((job) => job.id === nextJob.id);
  if (existingIndex < 0) {
    return [...jobs, nextJob];
  }
  return jobs.map((job) => (job.id === nextJob.id ? nextJob : job));
}

function resolveInitialDashboardTab(): Tab {
  if (typeof window === "undefined") return "dashboard";
  const hashTab = parseDashboardTab(window.location.hash);
  if (hashTab) return hashTab;
  const storedTab = parseDashboardTab(
    window.sessionStorage.getItem(DASHBOARD_TAB_STORAGE_KEY) ?? "",
  );
  if (storedTab) return storedTab;
  return "dashboard";
}

function parseDashboardTab(value: string): Tab | null {
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

function formatLibraryTimestamp(iso: string): string {
  try {
    const d = new Date(iso);
    const date = new Intl.DateTimeFormat("en-GB", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
    }).format(d);
    const time = new Intl.DateTimeFormat("en-GB", {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(d);
    return `${date} · ${time}`;
  } catch {
    return "";
  }
}

function formatAppVersionLabel(version?: string): string {
  if (!version) return "Version unavailable";
  return version.startsWith("v") ? version : `v${version}`;
}

function formatStatNumber(value?: number): string {
  if (typeof value !== "number") return "--";
  return new Intl.NumberFormat("en-GB").format(value);
}

function formatAverageWPM(value?: number): string {
  if (typeof value !== "number" || Number.isNaN(value) || value <= 0)
    return "--";
  return value.toFixed(1);
}

function formatRecordedMinutes(durationMs?: number): string {
  if (typeof durationMs !== "number" || durationMs <= 0) return "--";
  return (durationMs / 60000).toFixed(1);
}

function formatAudioDuration(durationMs: number) {
  const seconds = durationMs / 1000;
  if (seconds >= 60) return `${(seconds / 60).toFixed(1)}m`;
  return `${seconds.toFixed(1)}s`;
}

function formatTranscriptionModelLabel(model: string) {
  const normalized = model.trim();
  if (!normalized) return "";
  if (normalized.endsWith("whisper-large-v3-turbo")) return "turbo-v3";
  if (normalized.endsWith("whisper-large-v3")) return "large-v3";
  const leaf = normalized.split(/[\\/]/).pop() ?? normalized;
  return leaf.replace(/\.(bin|gguf|onnx)$/i, "");
}

function formatLogTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString("en-GB", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return "";
  }
}

function logColor(type: string): string {
  switch (type) {
    case "error":
      return "text-red-400";
    case "warn":
      return "text-yellow-400";
    case "success":
      return "text-green-400";
    default:
      return "text-[#938ea1]";
  }
}

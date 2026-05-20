import { Window } from "@wailsio/runtime";

import { DesktopWindowFrame } from "@/components/desktop-window-frame";
import { DashboardHomeView } from "@/components/dashboard/dashboard-home-view";
import { DashboardSidebar } from "@/components/dashboard/dashboard-sidebar";
import { LibraryView } from "@/components/dashboard/library-view";
import { LogsView } from "@/components/dashboard/logs-view";
import { ModelDownloadDock } from "@/components/dashboard/overlays/model-download-dock";
import { SetupWizard } from "@/components/dashboard/setup-wizard";
import { SpeechKitWindowIcon } from "@/components/dashboard/branding/speechkit-window-icon";
import {
  dashboardTabMeta,
  useDashboardShellState,
} from "@/components/dashboard/state-hooks/use-dashboard-shell-state";
import { SettingsApp } from "@/components/settings-app";

import { formatAppVersionLabel } from "./dashboard-formatters";

export function DashboardApp() {
  const {
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
  } = useDashboardShellState();

  if (showSetupWizard && setupChecked) {
    return (
      <div className="desktop-shell-root h-screen w-screen">
        <SetupWizard
          catalog={modelDownloads.catalog}
          jobs={modelDownloads.jobs}
          onStartDownload={modelDownloads.startDownload}
          onCancelDownload={modelDownloads.cancelDownload}
          onSelectDownloadedModel={modelDownloads.selectModel}
          onComplete={async (next) => {
            const response = await fetch("/app/complete-setup", {
              method: "POST",
            });
            if (!response.ok) {
              const detail = await response.text().catch(() => "");
              throw new Error(
                detail.trim() || `Setup completion failed: ${response.status}`,
              );
            }
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
            <DashboardHomeView
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

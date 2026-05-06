import { useAppUpdateState } from "@/components/dashboard/state-hooks/use-app-update-state";
import type { AppVersionInfo } from "@/lib/speechkit";

export function AppUpdateBanner({
  appVersionInfo,
}: {
  appVersionInfo: AppVersionInfo | null;
}) {
  const {
    latestVersion,
    updateURL,
    isManualUnsigned,
    statusLabel,
    latestJob,
    isRunning,
    isDone,
    showDownload,
    busyAction,
    actionError,
    handleDownload,
    handleCancel,
    handleOpen,
  } = useAppUpdateState(appVersionInfo);

  if (!latestVersion) return null;

  return (
    <div className="rounded-[22px] border border-[color:var(--sk-accent)]/18 bg-[color:var(--sk-accent-soft)] px-4 py-3 text-xs text-[color:var(--sk-accent)]">
      <div className="flex items-center gap-3">
        <span>Update available: v{latestVersion}</span>
        <span className="rounded-full border border-[color:var(--sk-accent)]/16 px-2 py-0.5 text-[11px] font-medium">
          {statusLabel}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {updateURL && (
            <a
              href={updateURL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-full border border-[color:var(--sk-accent)]/20 px-3 py-1 font-medium text-[color:var(--sk-accent)] transition-colors hover:bg-[color:var(--sk-accent)]/10"
            >
              {isManualUnsigned ? "Manual download" : "Change log"}
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

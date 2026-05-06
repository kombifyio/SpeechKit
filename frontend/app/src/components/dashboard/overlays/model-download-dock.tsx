import type { DownloadItem, DownloadJob } from "@/lib/speechkit";

import { pickLatestModelDownloadJob } from "@/components/dashboard/state-hooks/use-model-download-state";

export function ModelDownloadDock({
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

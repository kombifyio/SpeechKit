import type { Dispatch, SetStateAction } from "react";

import {
  startModelDownload,
  type DownloadItem,
  type DownloadJob,
} from "@/lib/speechkit";

export function DownloadConfirmationDialog({
  confirmItem,
  dlBusy,
  setConfirmItem,
  setDlBusy,
  setDlJobs,
  showToast,
}: {
  confirmItem: DownloadItem | null;
  dlBusy: boolean;
  setConfirmItem: Dispatch<SetStateAction<DownloadItem | null>>;
  setDlBusy: Dispatch<SetStateAction<boolean>>;
  setDlJobs: Dispatch<SetStateAction<DownloadJob[]>>;
  showToast: (message: string) => void;
}) {
  if (!confirmItem) return null;

  return (
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
                    Requires the Ollama local provider
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
  );
}

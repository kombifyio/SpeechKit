import { useCallback, useEffect, useState } from "react";

import {
  cancelModelDownload,
  fetchDownloadCatalog,
  fetchDownloadJobs,
  selectDownloadedModel,
  startModelDownload,
  type DownloadItem,
  type DownloadJob,
} from "@/lib/speechkit";

export function useModelDownloadState() {
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

export function pickLatestModelDownloadJob(
  jobs: DownloadJob[],
): DownloadJob | null {
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

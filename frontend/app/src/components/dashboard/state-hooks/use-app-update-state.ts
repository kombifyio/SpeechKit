import { useEffect, useRef, useState } from "react";

import {
  cancelAppUpdateDownload,
  fetchAppUpdateJobs,
  openAppUpdateInstaller,
  startAppUpdateDownload,
  type AppUpdateJob,
  type AppVersionInfo,
} from "@/lib/speechkit";

export type AppUpdateInstallMode =
  | "verified_installable"
  | "manual_unsigned"
  | "unavailable";

export function pickLatestAppUpdateJob(
  jobs: AppUpdateJob[],
): AppUpdateJob | null {
  return jobs[jobs.length - 1] ?? null;
}

export function isActiveAppUpdateJob(job: AppUpdateJob): boolean {
  return job.status === "pending" || job.status === "running";
}

export function upsertAppUpdateJob(
  jobs: AppUpdateJob[],
  nextJob: AppUpdateJob,
): AppUpdateJob[] {
  const existingIndex = jobs.findIndex((job) => job.id === nextJob.id);
  if (existingIndex < 0) {
    return [...jobs, nextJob];
  }
  return jobs.map((job) => (job.id === nextJob.id ? nextJob : job));
}

export function mergeAppUpdateJobs(
  previous: AppUpdateJob[],
  next: AppUpdateJob[],
): AppUpdateJob[] {
  const nextByID = new Map(next.map((job) => [job.id, job]));
  const merged = previous.flatMap((job) => {
    const replacement = nextByID.get(job.id);
    if (replacement) {
      nextByID.delete(job.id);
      return [replacement];
    }
    return isActiveAppUpdateJob(job) ? [job] : [];
  });
  return [...merged, ...nextByID.values()];
}

export function resolveAppUpdateInstallMode(
  appVersionInfo: AppVersionInfo | null,
): AppUpdateInstallMode {
  return (
    appVersionInfo?.installMode ??
    (appVersionInfo?.downloadURL ? "verified_installable" : "unavailable")
  );
}

export function appUpdateStatusLabel(mode: AppUpdateInstallMode): string {
  switch (mode) {
    case "verified_installable":
      return "Verified installer available";
    case "manual_unsigned":
      return "Manual download required";
    default:
      return "Installer unavailable";
  }
}

export function useAppUpdateState(appVersionInfo: AppVersionInfo | null) {
  const latestVersion = appVersionInfo?.latestVersion;
  const updateURL = appVersionInfo?.updateURL;
  const downloadURL = appVersionInfo?.downloadURL;
  const installMode = resolveAppUpdateInstallMode(appVersionInfo);
  const isManualUnsigned = installMode === "manual_unsigned";
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
        setJobs((previous) =>
          mergeAppUpdateJobs(
            previous,
            next.filter((job) => job.version === latestVersion),
          ),
        );
      } catch {
        if (active) {
          setJobs((previous) => previous.filter(isActiveAppUpdateJob));
        }
      }
    };

    void loadJobs();

    const interval = window.setInterval(() => {
      const hasRunningJob = jobsRef.current.some(isActiveAppUpdateJob);
      if (hasRunningJob) {
        void loadJobs();
      }
    }, 1000);

    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [latestVersion]);

  const latestJob = pickLatestAppUpdateJob(jobs);
  const isRunning = latestJob ? isActiveAppUpdateJob(latestJob) : false;
  const isDone = latestJob?.status === "done";
  const showDownload =
    !isRunning &&
    !isDone &&
    installMode === "verified_installable" &&
    !!downloadURL;

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

  return {
    latestVersion,
    updateURL,
    installMode,
    isManualUnsigned,
    statusLabel: appUpdateStatusLabel(installMode),
    latestJob,
    isRunning,
    isDone,
    showDownload,
    busyAction,
    actionError,
    handleDownload,
    handleCancel,
    handleOpen,
  };
}

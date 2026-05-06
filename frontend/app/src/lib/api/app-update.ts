export type AppVersionInfo = {
  version: string;
  latestVersion?: string;
  updateURL?: string;
  downloadURL?: string;
  downloadSizeBytes?: number;
  assetName?: string;
  installMode?: "verified_installable" | "manual_unsigned" | "unavailable";
  manualDownloadRequired?: boolean;
};

export type AppUpdateStatus =
  | "pending"
  | "running"
  | "done"
  | "failed"
  | "cancelled";

export type AppUpdateJob = {
  id: string;
  version: string;
  assetName: string;
  status: AppUpdateStatus;
  progress: number;
  bytesDone: number;
  totalBytes: number;
  statusText: string;
  filePath?: string;
  error?: string;
};

export async function fetchAppVersion(): Promise<AppVersionInfo> {
  const response = await fetch("/app/version", { cache: "no-store" });
  if (!response.ok) throw new Error(`app version: ${response.status}`);
  return (await response.json()) as AppVersionInfo;
}

export async function fetchAppUpdateJobs(): Promise<AppUpdateJob[]> {
  const response = await fetch("/app/update/jobs", { cache: "no-store" });
  if (!response.ok) throw new Error(`app update jobs: ${response.status}`);
  return (await response.json()) as AppUpdateJob[];
}

export async function startAppUpdateDownload(
  version: string,
): Promise<AppUpdateJob> {
  const body = new URLSearchParams({ version });
  const response = await fetch("/app/update/download", {
    method: "POST",
    body,
  });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `app update download: ${response.status}`);
  }
  return (await response.json()) as AppUpdateJob;
}

export async function cancelAppUpdateDownload(jobId: string): Promise<void> {
  const body = new URLSearchParams({ job_id: jobId });
  const response = await fetch("/app/update/cancel", { method: "POST", body });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `app update cancel: ${response.status}`);
  }
}

export async function openAppUpdateInstaller(
  jobId: string,
): Promise<{ message?: string; filePath?: string }> {
  const body = new URLSearchParams({ job_id: jobId });
  const response = await fetch("/app/update/open", { method: "POST", body });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `app update open: ${response.status}`);
  }
  return (await response.json()) as { message?: string; filePath?: string };
}

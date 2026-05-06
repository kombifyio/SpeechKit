import type { DownloadItem, DownloadJob } from "./types";

export async function fetchDownloadCatalog(): Promise<DownloadItem[]> {
  const resp = await fetch("/models/downloads/catalog");
  if (!resp.ok) throw new Error(`catalog fetch failed: ${resp.status}`);
  return resp.json() as Promise<DownloadItem[]>;
}

export async function fetchDownloadJobs(): Promise<DownloadJob[]> {
  const resp = await fetch("/models/downloads/jobs");
  if (!resp.ok) throw new Error(`jobs fetch failed: ${resp.status}`);
  return resp.json() as Promise<DownloadJob[]>;
}

export async function startModelDownload(
  modelId: string,
): Promise<DownloadJob> {
  const body = new URLSearchParams({ model_id: modelId });
  const resp = await fetch("/models/downloads/start", { method: "POST", body });
  if (!resp.ok) {
    const err = await resp.text();
    throw new Error(err || `start download failed: ${resp.status}`);
  }
  return resp.json() as Promise<DownloadJob>;
}

export async function selectDownloadedModel(
  modelId: string,
): Promise<{ message?: string }> {
  const body = new URLSearchParams({ model_id: modelId });
  const resp = await fetch("/models/downloads/select", {
    method: "POST",
    body,
  });
  if (!resp.ok) {
    const err = await resp.text();
    throw new Error(err || `select model failed: ${resp.status}`);
  }
  return resp.json() as Promise<{ message?: string }>;
}

export async function cancelModelDownload(jobId: string): Promise<void> {
  const body = new URLSearchParams({ job_id: jobId });
  await fetch("/models/downloads/cancel", { method: "POST", body });
}

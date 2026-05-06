import type {
  DashboardStats,
  LogEntry,
  QuickNote,
  TranscriptionRecord,
} from "./types";

export async function fetchHistory(): Promise<TranscriptionRecord[]> {
  const response = await fetch("/dashboard/history", { cache: "no-store" });
  if (!response.ok) throw new Error(`history: ${response.status}`);
  return (await response.json()) as TranscriptionRecord[];
}

export async function fetchDashboardStats(): Promise<DashboardStats> {
  const response = await fetch("/dashboard/stats", { cache: "no-store" });
  if (!response.ok) throw new Error(`dashboard stats: ${response.status}`);
  return (await response.json()) as DashboardStats;
}

export async function fetchLogs(): Promise<LogEntry[]> {
  const response = await fetch("/dashboard/logs", { cache: "no-store" });
  if (!response.ok) throw new Error(`logs: ${response.status}`);
  return (await response.json()) as LogEntry[];
}

export async function fetchQuickNotes(): Promise<QuickNote[]> {
  const response = await fetch("/dashboard/quicknotes", { cache: "no-store" });
  if (!response.ok) throw new Error(`quicknotes: ${response.status}`);
  return (await response.json()) as QuickNote[];
}

export async function createQuickNote(
  text: string,
): Promise<{ id: number; message: string }> {
  const body = new URLSearchParams({ text });
  const response = await fetch("/quicknotes/create", { method: "POST", body });
  if (!response.ok) throw new Error(`create quicknote: ${response.status}`);
  return (await response.json()) as { id: number; message: string };
}

export async function updateQuickNote(
  id: number,
  text: string,
): Promise<string> {
  const body = new URLSearchParams({ id: String(id), text });
  const response = await fetch("/quicknotes/update", { method: "POST", body });
  if (!response.ok) throw new Error(`update quicknote: ${response.status}`);
  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export async function pinQuickNote(
  id: number,
  pinned: boolean,
): Promise<string> {
  const body = new URLSearchParams({
    id: String(id),
    pinned: pinned ? "1" : "0",
  });
  const response = await fetch("/quicknotes/pin", { method: "POST", body });
  if (!response.ok) throw new Error(`pin quicknote: ${response.status}`);
  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export async function deleteQuickNote(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) });
  const response = await fetch("/quicknotes/delete", { method: "POST", body });
  if (!response.ok) throw new Error(`delete quicknote: ${response.status}`);
  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export async function quickNoteSummary(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) });
  const response = await fetch("/quicknotes/summary", { method: "POST", body });
  if (!response.ok) throw new Error(`quicknote summary: ${response.status}`);
  const payload = (await response.json()) as { summary: string };
  return payload.summary;
}

export async function quickNoteEmail(id: number): Promise<string> {
  const body = new URLSearchParams({ id: String(id) });
  const response = await fetch("/quicknotes/email", { method: "POST", body });
  if (!response.ok) throw new Error(`quicknote email: ${response.status}`);
  const payload = (await response.json()) as { email: string };
  return payload.email;
}

export async function armQuickNoteRecording(noteId?: number): Promise<string> {
  const suffix =
    typeof noteId === "number" && noteId > 0 ? `?id=${noteId}` : "";
  const response = await fetch(`/quicknotes/record-mode${suffix}`, {
    method: "POST",
  });
  if (!response.ok) throw new Error(`arm quicknote: ${response.status}`);
  const payload = (await response.json()) as { message: string };
  return payload.message;
}

export function dashboardAudioDownloadURL(
  kind: "transcription" | "quicknote",
  id: number,
) {
  return `/dashboard/audio?kind=${kind}&id=${id}`;
}

export async function revealDashboardAudio(
  kind: "transcription" | "quicknote",
  id: number,
): Promise<string> {
  const body = new URLSearchParams({
    kind,
    id: String(id),
  });
  const response = await fetch("/dashboard/audio/reveal", {
    method: "POST",
    body,
  });
  if (!response.ok) {
    throw new Error(`audio reveal: ${response.status}`);
  }
  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export {
  cancelAppUpdateDownload,
  fetchAppUpdateJobs,
  fetchAppVersion,
  openAppUpdateInstaller,
  startAppUpdateDownload,
  type AppUpdateJob,
  type AppUpdateStatus,
  type AppVersionInfo,
} from "../api/app-update";

export function sortByNewest<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort(
    (a, b) => new Date(getDate(b)).getTime() - new Date(getDate(a)).getTime(),
  );
}

export function formatLibraryTimestamp(iso: string): string {
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

export function formatAppVersionLabel(version?: string): string {
  if (!version) return "Version unavailable";
  return version.startsWith("v") ? version : `v${version}`;
}

export function formatStatNumber(value?: number): string {
  if (typeof value !== "number") return "--";
  return new Intl.NumberFormat("en-GB").format(value);
}

export function formatAverageWPM(value?: number): string {
  if (typeof value !== "number" || Number.isNaN(value) || value <= 0)
    return "--";
  return value.toFixed(1);
}

export function formatRecordedMinutes(durationMs?: number): string {
  if (typeof durationMs !== "number" || durationMs <= 0) return "--";
  return (durationMs / 60000).toFixed(1);
}

export function formatAudioDuration(durationMs: number) {
  const seconds = durationMs / 1000;
  if (seconds >= 60) return `${(seconds / 60).toFixed(1)}m`;
  return `${seconds.toFixed(1)}s`;
}

export function formatTranscriptionModelLabel(model: string) {
  const normalized = model.trim();
  if (!normalized) return "";
  if (normalized.endsWith("whisper-large-v3-turbo")) return "turbo-v3";
  if (normalized.endsWith("whisper-large-v3")) return "large-v3";
  const leaf = normalized.split(/[\\/]/).pop() ?? normalized;
  return leaf.replace(/\.(bin|gguf|onnx)$/i, "");
}

export function formatVoiceRuntimeKind(runtimeKind?: string) {
  switch (runtimeKind) {
    case "native_realtime":
      return "Gemini Live";
    case "pipeline_fallback":
      return "Pipeline fallback";
    default:
      return runtimeKind?.trim() || "Voice Agent";
  }
}

export function formatLogTime(iso: string): string {
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

export function logColor(type: string): string {
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

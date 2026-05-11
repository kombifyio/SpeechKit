import { useCallback, useEffect, useRef, useState } from "react";

import { fetchLogs, type LogEntry } from "@/lib/speechkit";

import { formatLogTime, logColor } from "./dashboard-formatters";

export function LogsView() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);

  const loadLogs = useCallback(async () => {
    try {
      return await fetchLogs();
    } catch {
      return null;
    }
  }, []);

  useEffect(() => {
    let active = true;
    const syncLogs = async () => {
      const entries = await loadLogs();
      if (!active) return;
      if (entries) setLogs(entries);
      setLoading(false);
    };
    void syncLogs();
    const timer = window.setInterval(() => void syncLogs(), 2000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [loadLogs]);

  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [logs]);

  return (
    <div className="flex h-full flex-col">
      <div
        ref={containerRef}
        className="flex-1 overflow-y-auto bg-[color:var(--sk-surface-0)] px-8 py-8 font-mono text-xs leading-relaxed text-[color:var(--sk-text)]"
      >
        <h2 className="mb-4 font-sans text-sm font-semibold text-[color:var(--sk-accent)]">
          Application Logs
        </h2>
        {loading && (
          <p className="text-[color:var(--sk-text-muted)]">Loading logs...</p>
        )}
        {!loading && logs.length === 0 && (
          <p className="text-[color:var(--sk-text-muted)]">No log entries.</p>
        )}
        {logs.map((entry, i) => (
          <div key={`${entry.timestamp}-${i}`} className="flex gap-2">
            <span className="shrink-0 text-[#938ea1]/50">
              {formatLogTime(entry.timestamp)}
            </span>
            <span className={logColor(entry.type)}>{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ── Setup Wizard ── */

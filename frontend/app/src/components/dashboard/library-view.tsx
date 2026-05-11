import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  dashboardAudioDownloadURL,
  deleteQuickNote,
  fetchAPIV1VoiceSessions,
  fetchHistory,
  fetchQuickNotes,
  pinQuickNote,
  revealDashboardAudio,
  type QuickNote,
  type TranscriptionRecord,
  type VoiceAgentSessionRecord,
} from "@/lib/speechkit";

import {
  formatAudioDuration,
  formatLibraryTimestamp,
  formatTranscriptionModelLabel,
  formatVoiceRuntimeKind,
  sortByNewest,
} from "./dashboard-formatters";

export function LibraryView() {
  const [history, setHistory] = useState<TranscriptionRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [copiedId, setCopiedId] = useState<number | null>(null);
  const copyTimer = useRef<number | null>(null);
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([]);
  const [copiedNote, setCopiedNote] = useState<number | null>(null);
  const [voiceSessions, setVoiceSessions] = useState<VoiceAgentSessionRecord[]>(
    [],
  );
  const [voiceSessionsLoading, setVoiceSessionsLoading] = useState(true);
  const sortedHistory = useMemo(
    () => sortByNewest(history, (r) => r.createdAt),
    [history],
  );
  const sortedQuickNotes = useMemo(
    () => sortByNewest(quickNotes, (n) => n.createdAt),
    [quickNotes],
  );
  const pinnedQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => n.pinned),
    [sortedQuickNotes],
  );
  const recentQuickNotes = useMemo(
    () => sortedQuickNotes.filter((n) => !n.pinned),
    [sortedQuickNotes],
  );
  const sortedVoiceSessions = useMemo(
    () => sortByNewest(voiceSessions, (session) => session.createdAt || session.endedAt || session.startedAt),
    [voiceSessions],
  );

  useEffect(() => {
    let active = true;
    void fetchHistory()
      .then((records) => {
        if (!active) return;
        setHistory(records);
        setLoading(false);
      })
      .catch(() => {
        if (!active) return;
        setLoading(false);
      });
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes);
      })
      .catch(() => {});
    void fetchAPIV1VoiceSessions(20)
      .then((sessions) => {
        if (!active) return;
        setVoiceSessions(sessions);
        setVoiceSessionsLoading(false);
      })
      .catch(() => {
        if (active) setVoiceSessionsLoading(false);
      });
    return () => {
      active = false;
      if (copyTimer.current) window.clearTimeout(copyTimer.current);
    };
  }, []);

  const copyText = useCallback((id: number, text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id);
      if (copyTimer.current) window.clearTimeout(copyTimer.current);
      copyTimer.current = window.setTimeout(() => setCopiedId(null), 1200);
    });
  }, []);

  const handlePinNote = useCallback(async (id: number, pinned: boolean) => {
    try {
      await pinQuickNote(id, pinned);
      const notes = await fetchQuickNotes();
      setQuickNotes(notes);
    } catch {
      return;
    }
  }, []);

  const handleDeleteNote = useCallback(async (id: number) => {
    try {
      await deleteQuickNote(id);
      const notes = await fetchQuickNotes();
      setQuickNotes(notes);
    } catch {
      return;
    }
  }, []);

  const handleCopyNote = useCallback((id: number, text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopiedNote(id);
      if (copyTimer.current) window.clearTimeout(copyTimer.current);
      copyTimer.current = window.setTimeout(() => setCopiedNote(null), 1200);
    });
  }, []);

  return (
    <div className="flex h-full flex-col">
      <div className="grid min-h-0 flex-1 gap-4 px-8 py-8 xl:grid-cols-[1.05fr_0.95fr_1.05fr]">
        {/* Left: Transcriptions */}
        <div className="flex min-h-0 flex-1 flex-col">
          <span className="mb-3 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
            Recent Transcriptions
          </span>
          <div className="sk-panel flex-1 overflow-y-auto rounded-[24px] p-1">
            {loading && (
              <p className="py-4 text-center text-xs text-[color:var(--sk-text-muted)]">
                Loading...
              </p>
            )}
            {!loading && sortedHistory.length === 0 && (
              <p className="py-8 text-center text-xs text-[color:var(--sk-text-muted)]">
                No transcriptions yet. Press your hotkey to start.
              </p>
            )}
            {!loading && sortedHistory.length > 0 && (
              <div className="flex flex-col gap-0.5">
                {sortedHistory.map((record) => (
                  <TranscriptionRow
                    key={record.id}
                    record={record}
                    copied={copiedId === record.id}
                    onCopy={copyText}
                    onRevealAudio={revealDashboardAudio}
                  />
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Right: Quick Notes */}
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex items-center justify-between mb-3">
            <span className="text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
              Quick Notes
            </span>
            <button
              type="button"
              onClick={() =>
                fetch("/quicknotes/open-editor", { method: "POST" })
              }
              className="sk-primary-button rounded-full px-4 py-1.5 text-xs font-bold transition-all hover:opacity-90"
            >
              + New
            </button>
          </div>
          <div className="sk-panel flex-1 overflow-y-auto rounded-[24px] p-3">
            {sortedQuickNotes.length === 0 && (
              <p className="py-4 text-center text-xs text-[color:var(--sk-text-muted)]">
                No quick notes yet.
              </p>
            )}
            <div className="flex flex-col gap-1.5">
              {pinnedQuickNotes.length > 0 && (
                <>
                  <span className="mb-1 mt-0.5 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-accent)]/80">
                    Pinned Notes
                  </span>
                  {pinnedQuickNotes.map((note) => (
                    <QuickNoteRow
                      key={note.id}
                      note={note}
                      copied={copiedNote === note.id}
                      onCopy={handleCopyNote}
                      onDelete={handleDeleteNote}
                      onPin={handlePinNote}
                      onRevealAudio={revealDashboardAudio}
                    />
                  ))}
                  {recentQuickNotes.length > 0 && (
                    <span className="mb-1 mt-2 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]/80">
                      Recent Notes
                    </span>
                  )}
                </>
              )}
              {(pinnedQuickNotes.length > 0
                ? recentQuickNotes
                : sortedQuickNotes
              ).map((note) => (
                <QuickNoteRow
                  key={note.id}
                  note={note}
                  copied={copiedNote === note.id}
                  onCopy={handleCopyNote}
                  onDelete={handleDeleteNote}
                  onPin={handlePinNote}
                  onRevealAudio={revealDashboardAudio}
                />
              ))}
            </div>
          </div>
        </div>

        {/* Voice Agent sessions */}
        <div className="flex min-h-0 flex-1 flex-col">
          <span className="mb-3 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
            Voice Sessions
          </span>
          <div className="sk-panel flex-1 overflow-y-auto rounded-[24px] p-3">
            {voiceSessionsLoading && (
              <p className="py-4 text-center text-xs text-[color:var(--sk-text-muted)]">
                Loading...
              </p>
            )}
            {!voiceSessionsLoading && sortedVoiceSessions.length === 0 && (
              <p className="py-8 text-center text-xs text-[color:var(--sk-text-muted)]">
                No voice sessions yet.
              </p>
            )}
            {!voiceSessionsLoading && sortedVoiceSessions.length > 0 && (
              <div className="flex flex-col gap-1.5">
                {sortedVoiceSessions.map((session) => (
                  <VoiceSessionRow key={session.id} session={session} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Logs View ── */

function VoiceSessionRow({ session }: { session: VoiceAgentSessionRecord }) {
  const summary = session.summary;
  const sessionTitle =
    summary?.title?.trim() || `Voice session #${session.id}`;
  const summaryText =
    summary?.summary?.trim() || session.transcript?.trim() || "No summary saved.";
  const timestamp = session.createdAt || session.endedAt || session.startedAt;
  const turnCount = session.turns?.length ?? 0;

  return (
    <div
      data-testid="voice-session-row"
      className="rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2.5"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-[color:var(--sk-text)]">
            {sessionTitle}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <span className="rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)]">
              {formatVoiceRuntimeKind(session.runtimeKind)}
            </span>
            {turnCount > 0 && (
              <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]">
                {turnCount} turns
              </span>
            )}
          </div>
        </div>
        <span className="shrink-0 text-[10px] text-[color:var(--sk-text-muted)]">
          {formatLibraryTimestamp(timestamp)}
        </span>
      </div>
      <p className="mt-2 line-clamp-4 text-xs leading-relaxed text-[color:var(--sk-text)]/75">
        {summaryText}
      </p>
      <VoiceSessionSummaryList label="Decisions" items={summary?.decisions} />
      <VoiceSessionSummaryList label="Next" items={summary?.nextSteps} />
      <VoiceSessionSummaryList label="Open" items={summary?.openQuestions} />
    </div>
  );
}
function VoiceSessionSummaryList({
  label,
  items,
}: {
  label: string;
  items?: string[];
}) {
  const visibleItems = (items ?? []).filter(Boolean).slice(0, 2);
  if (visibleItems.length === 0) return null;
  return (
    <div className="mt-2 flex flex-wrap gap-1.5">
      <span className="rounded bg-[color:var(--sk-surface-2)] px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-[color:var(--sk-text-muted)]">
        {label}
      </span>
      {visibleItems.map((item) => (
        <span
          key={`${label}-${item}`}
          className="max-w-full truncate rounded bg-[color:var(--sk-surface-2)] px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)]"
        >
          {item}
        </span>
      ))}
    </div>
  );
}
function QuickNoteRow({
  note,
  copied,
  onCopy,
  onDelete,
  onPin,
  onRevealAudio,
}: {
  note: QuickNote;
  copied: boolean;
  onCopy: (id: number, text: string) => void;
  onDelete: (id: number) => void;
  onPin: (id: number, pinned: boolean) => Promise<void>;
  onRevealAudio: (
    kind: "transcription" | "quicknote",
    id: number,
  ) => Promise<string>;
}) {
  return (
    <div
      data-testid="quicknote-row"
      className="group rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2"
    >
      <p className="line-clamp-3 text-xs leading-relaxed text-[color:var(--sk-text)]/75">
        {note.text}
      </p>
      <div className="mt-1.5 flex items-center gap-2">
        <span className="text-[10px] text-[color:var(--sk-text-muted)]">
          {formatLibraryTimestamp(note.createdAt)}
        </span>
        {note.pinned && (
          <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]">
            Pinned
          </span>
        )}
        {note.provider && note.provider !== "manual" && (
          <span className="rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px] text-[color:var(--sk-text-muted)]">
            {note.provider}
          </span>
        )}
        {note.audio && (
          <span className="rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
            {formatAudioDuration(note.audio.durationMs)}
          </span>
        )}
        <div className="ml-auto flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
          {note.audio && (
            <>
              <a
                href={dashboardAudioDownloadURL("quicknote", note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
                aria-label="Download audio"
              >
                Download audio
              </a>
              <button
                type="button"
                onClick={() => void onRevealAudio("quicknote", note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
                aria-label="Show file"
              >
                Show file
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => void onPin(note.id, !note.pinned)}
            className={`rounded px-1.5 py-0.5 text-[10px] ${
              note.pinned
                ? "text-[color:var(--sk-accent)] hover:bg-[color:var(--sk-accent-soft)]"
                : "text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
            }`}
          >
            {note.pinned ? "Unpin" : "Pin"}
          </button>
          <button
            type="button"
            onClick={() =>
              fetch(`/quicknotes/open-editor?id=${note.id}`, { method: "POST" })
            }
            className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-accent)]/70 hover:bg-[color:var(--sk-accent-soft)] hover:text-[color:var(--sk-accent)]"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => onCopy(note.id, note.text)}
            className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
          >
            {copied ? "Copied!" : "Copy"}
          </button>
          <button
            type="button"
            onClick={() => onDelete(note.id)}
            className="rounded px-1.5 py-0.5 text-[10px] text-red-400/60 hover:bg-red-500/10 hover:text-red-400"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
function TranscriptionRow({
  record,
  copied,
  onCopy,
  onRevealAudio,
}: {
  record: TranscriptionRecord;
  copied: boolean;
  onCopy: (id: number, text: string) => void;
  onRevealAudio: (
    kind: "transcription" | "quicknote",
    id: number,
  ) => Promise<string>;
}) {
  return (
    <div
      data-testid="transcription-row"
      className="group flex items-start gap-3 rounded-[18px] px-3 py-2.5 transition-colors hover:bg-[color:var(--sk-surface-2)]/70"
    >
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm leading-snug text-[color:var(--sk-text)]/82">
          {record.text}
        </p>
        <div className="mt-1 flex items-center gap-1.5 overflow-hidden">
          <span className="shrink-0 rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px] font-medium text-[color:var(--sk-text-muted)]">
            {record.provider}
          </span>
          {record.model && (
            <span className="shrink-0 truncate rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[10px] text-[color:var(--sk-accent)]">
              {formatTranscriptionModelLabel(record.model)}
            </span>
          )}
          {record.audio && (
            <span className="shrink-0 rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
              {formatAudioDuration(record.audio.durationMs)}
            </span>
          )}
          <span className="shrink-0 text-[11px] text-[color:var(--sk-text-muted)]">
            {formatLibraryTimestamp(record.createdAt)}
          </span>
        </div>
      </div>
      <div className="mt-0.5 flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {record.audio && (
          <>
            <a
              href={dashboardAudioDownloadURL("transcription", record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
              aria-label="Download audio"
            >
              Download audio
            </a>
            <button
              type="button"
              onClick={() => void onRevealAudio("transcription", record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]"
              aria-label="Show file"
            >
              Show file
            </button>
          </>
        )}
        <button
          type="button"
          onClick={() => onCopy(record.id, record.text)}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[color:var(--sk-text-muted)] transition-colors hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-accent)]"
          title="Copy to clipboard"
        >
          {copied ? (
            <span className="text-[10px] font-medium text-emerald-400">
              Copied!
            </span>
          ) : (
            <ClipboardIcon />
          )}
        </button>
      </div>
    </div>
  );
}

function ClipboardIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

/* ── Utilities ── */

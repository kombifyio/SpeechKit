import { useEffect, useMemo, useState } from "react";

import { AppUpdateBanner } from "@/components/dashboard/app-update-banner";
import {
  fetchDashboardStats,
  fetchHistory,
  fetchQuickNotes,
  type AppVersionInfo,
  type DashboardStats,
  type QuickNote,
  type TranscriptionRecord,
} from "@/lib/speechkit";

import {
  formatAverageWPM,
  formatLibraryTimestamp,
  formatRecordedMinutes,
  formatStatNumber,
  formatTranscriptionModelLabel,
  sortByNewest,
} from "./dashboard-formatters";

export function DashboardHomeView({
  appVersionInfo,
  onOpenLibrary,
  onOpenSettings,
}: {
  appVersionInfo: AppVersionInfo | null;
  onOpenLibrary: () => void;
  onOpenSettings: () => void;
}) {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [history, setHistory] = useState<TranscriptionRecord[]>([]);
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([]);

  useEffect(() => {
    let active = true;
    void fetchDashboardStats()
      .then((next) => {
        if (active) setStats(next);
      })
      .catch(() => {
        if (active) setStats(null);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;
    void fetchHistory()
      .then((records) => {
        if (active) setHistory(records);
      })
      .catch(() => {
        if (active) setHistory([]);
      });
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes);
      })
      .catch(() => {
        if (active) setQuickNotes([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const sortedHistory = useMemo(
    () => sortByNewest(history, (r) => r.createdAt),
    [history],
  );
  const sortedQuickNotes = useMemo(
    () => sortByNewest(quickNotes, (n) => n.createdAt),
    [quickNotes],
  );
  const latestTranscription = sortedHistory[0] ?? null;
  const pinnedNotes = sortedQuickNotes.filter((n) => n.pinned);
  const featuredNotes =
    pinnedNotes.length > 0
      ? pinnedNotes.slice(0, 3)
      : sortedQuickNotes.slice(0, 3);

  return (
    <div
      data-testid="welcome-scroll"
      className="h-full overflow-y-auto px-8 py-8"
    >
      <div className="space-y-8 pb-12">
        <AppUpdateBanner appVersionInfo={appVersionInfo} />

        {/* KPI Row */}
        <div data-testid="welcome-kpis" className="grid grid-cols-4 gap-4">
          <KPICard
            label="Total Recordings"
            value={formatStatNumber(stats?.transcriptions)}
          />
          <KPICard
            label="Average WPM"
            value={formatAverageWPM(stats?.averageWordsPerMinute)}
          />
          <KPICard
            label="Total Words"
            value={formatStatNumber(stats?.totalWords)}
          />
          <KPICard
            label="Recorded Minutes"
            value={formatRecordedMinutes(stats?.totalAudioDurationMs)}
          />
        </div>

        {/* Recent activity: transcriptions + notes */}
        {latestTranscription || featuredNotes.length > 0 ? (
          <div>
            <h3 className="mb-4 text-xs font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
              Recent Activity
            </h3>
            <div className="grid gap-6 md:grid-cols-[1.4fr_1fr]">
              {/* Latest transcription */}
              <section className="sk-panel rounded-[24px] p-6">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                  Latest transcription
                </p>
                <h3 className="text-lg font-semibold text-[color:var(--sk-text)]">
                  Latest capture
                </h3>
                {latestTranscription ? (
                  <>
                    <p className="mt-4 text-sm leading-7 text-[color:var(--sk-text)]/85">
                      {latestTranscription.text}
                    </p>
                    <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-[color:var(--sk-text-muted)]">
                      <span>
                        {formatLibraryTimestamp(latestTranscription.createdAt)}
                      </span>
                      <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[color:var(--sk-accent)]">
                        {latestTranscription.provider}
                      </span>
                      {latestTranscription.model && (
                        <span className="rounded-full bg-[color:var(--sk-surface-0)] px-2 py-0.5 text-[color:var(--sk-text)]/80">
                          {formatTranscriptionModelLabel(
                            latestTranscription.model,
                          )}
                        </span>
                      )}
                    </div>
                  </>
                ) : (
                  <p className="mt-4 text-sm text-[color:var(--sk-text-muted)]">
                    No transcriptions yet.
                  </p>
                )}
              </section>

              {/* Quick notes */}
              <section className="sk-panel rounded-[24px] p-6">
                <p className="mb-1 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                  Pinned notes
                </p>
                <h3 className="text-lg font-semibold text-[color:var(--sk-text)]">
                  Fast recall
                </h3>
                <div className="mt-4 flex flex-col gap-2">
                  {featuredNotes.map((note) => (
                    <div
                      key={note.id}
                      className="rounded-[18px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-4 py-3"
                    >
                      <p className="line-clamp-3 text-sm leading-6 text-[color:var(--sk-text)]/80">
                        {note.text}
                      </p>
                      <div className="mt-2 flex items-center gap-2 text-[10px] text-[color:var(--sk-text-muted)]">
                        <span>{formatLibraryTimestamp(note.createdAt)}</span>
                        {note.pinned && (
                          <span className="rounded-full bg-[color:var(--sk-accent-soft)] px-2 py-0.5 text-[color:var(--sk-accent)]">
                            Pinned
                          </span>
                        )}
                      </div>
                    </div>
                  ))}
                  {featuredNotes.length === 0 && (
                    <p className="text-sm text-[color:var(--sk-text-muted)]">
                      Create a quick note to keep names, snippets, or follow-ups
                      close.
                    </p>
                  )}
                </div>
              </section>
            </div>
          </div>
        ) : (
          /* Empty state / Quick Start */
          <div className="sk-panel rounded-[28px] p-8">
            <h3 className="text-xl font-semibold text-[color:var(--sk-text)]">
              Welcome to SpeechKit
            </h3>
            <p className="mt-2 max-w-[50ch] text-sm text-[color:var(--sk-text-muted)]">
              SpeechKit stays close to the edge of your screen, keeps quick
              notes nearby, and lets you move from a short thought to a full
              dictation without opening a heavy dashboard.
            </p>

            <div className="mt-7">
              <h4 className="text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
                Quick Start
              </h4>
              <div className="mt-3 grid gap-3">
                <QuickStartCard number="01" title="Hold Windows Alt to talk">
                  Start dictation anywhere, keep speaking naturally, then
                  release when done.
                </QuickStartCard>
                <QuickStartCard number="02" title="Hover over the pill">
                  Create a quick note from the hover menu, or speak directly
                  into capture.
                </QuickStartCard>
                <QuickStartCard
                  number="03"
                  title="Say Summarize on selected text"
                >
                  Quick words trigger focused actions on the current selection.
                </QuickStartCard>
              </div>
            </div>

            <div className="mt-6 flex flex-wrap gap-2">
              <button
                type="button"
                onClick={onOpenLibrary}
                className="sk-primary-button rounded-full px-5 py-2 text-xs font-bold transition-all hover:opacity-90"
              >
                Open Library
              </button>
              <button
                type="button"
                onClick={onOpenSettings}
                className="sk-secondary-button rounded-full px-4 py-2 text-xs font-medium transition-colors"
              >
                Open Settings
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/* ── Library View ── */

function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="sk-panel rounded-[24px] p-5 transition-all hover:bg-[color:var(--sk-surface-2)]">
      <p className="mb-2 text-[10px] font-bold uppercase tracking-widest text-[color:var(--sk-text-muted)]">
        {label}
      </p>
      <span className="text-2xl font-bold text-[color:var(--sk-text)]">
        {value}
      </span>
    </div>
  );
}

function QuickStartCard({
  number,
  title,
  children,
}: {
  number: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-start gap-3 rounded-[20px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-4 py-3">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[color:var(--sk-surface-2)] text-[color:var(--sk-accent)]">
        <span className="text-xs font-bold">{number}</span>
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-[color:var(--sk-text)]">
          {title}
        </p>
        <p className="mt-1 text-xs leading-6 text-[color:var(--sk-text-muted)]">
          {children}
        </p>
      </div>
    </div>
  );
}

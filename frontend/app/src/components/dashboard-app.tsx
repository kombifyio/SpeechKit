import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { SettingsApp } from '@/components/settings-app'
import {
  dashboardAudioDownloadURL,
  deleteQuickNote,
  fetchAudioDevices,
  fetchDashboardStats,
  fetchHistory,
  fetchLogs,
  fetchQuickNotes,
  setAudioDevice,
  type AudioDevice,
  type DashboardStats,
  pinQuickNote,
  type LogEntry,
  type QuickNote,
  revealDashboardAudio,
  type TranscriptionRecord,
} from '@/lib/speechkit'

type Tab = 'welcome' | 'library' | 'settings' | 'logs'

const DASHBOARD_TAB_STORAGE_KEY = 'speechkit.dashboard.tab'

export function DashboardApp() {
  const [tab, setTab] = useState<Tab>(() => resolveInitialDashboardTab())
  const [showSetupWizard, setShowSetupWizard] = useState(false)
  const [setupChecked, setSetupChecked] = useState(false)
  const [toasts, setToasts] = useState<Array<{ id: number; message: string; type: 'error' | 'warn' | 'success' }>>([])
  const toastIdRef = useRef(0)

  useEffect(() => {
    let active = true
    void fetch('/app/setup-status')
      .then(r => r.json())
      .then(data => {
        if (active) {
          setShowSetupWizard(!data.setupDone)
          setSetupChecked(true)
        }
      })
      .catch(() => {
        if (active) setSetupChecked(true)
      })
    return () => { active = false }
  }, [])

  const addToast = useCallback((message: string, type: 'error' | 'warn' | 'success' = 'error') => {
    const id = ++toastIdRef.current
    setToasts(prev => [...prev.slice(-4), { id, message, type }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 5000)
  }, [])

  const lastLogCountRef = useRef(0)
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const logs = await fetchLogs()
        if (logs.length > lastLogCountRef.current) {
          const newLogs = logs.slice(lastLogCountRef.current)
          for (const log of newLogs) {
            if (log.type === 'error') {
              addToast(log.message, 'error')
            }
          }
          lastLogCountRef.current = logs.length
        }
      } catch { /* ignore */ }
    }, 3000)
    return () => clearInterval(interval)
  }, [addToast])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return
    }
    window.sessionStorage.setItem(DASHBOARD_TAB_STORAGE_KEY, tab)
    const nextURL = new URL(window.location.href)
    nextURL.hash = tab === 'welcome' ? '' : `#${tab}`
    window.history.replaceState({}, '', `${nextURL.pathname}${nextURL.search}${nextURL.hash}`)
  }, [tab])

  if (showSetupWizard && setupChecked) {
    return (
      <SetupWizard
        onComplete={() => {
          void fetch('/app/complete-setup', { method: 'POST' })
          setShowSetupWizard(false)
        }}
      />
    )
  }

  if (!setupChecked) {
    return <div className="flex h-screen items-center justify-center bg-[#0b0f14] text-white/40 text-sm">Loading...</div>
  }

  return (
    <div className="flex h-screen flex-col bg-[#0b0f14] text-[13px] text-white/90">
      <div className="flex-shrink-0 px-5 pt-5">
        {/* Tab navigation */}
        <div className="flex gap-px rounded-lg bg-white/5 p-0.5">
          <TabBtn active={tab === 'welcome'} onClick={() => setTab('welcome')}>
            Welcome
          </TabBtn>
          <TabBtn active={tab === 'library'} onClick={() => setTab('library')}>
            Library
          </TabBtn>
          <TabBtn active={tab === 'settings'} onClick={() => setTab('settings')}>
            Settings
          </TabBtn>
          <TabBtn active={tab === 'logs'} onClick={() => setTab('logs')}>
            Logs
          </TabBtn>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        {tab === 'welcome' && <WelcomeTab onOpenLibrary={() => setTab('library')} />}
        {tab === 'library' && <LibraryTab />}
        {tab === 'settings' && (
          <div className="h-full overflow-y-auto">
            <SettingsApp />
          </div>
        )}
        {tab === 'logs' && <LogsTab />}
      </div>

      {/* Toast container */}
      {toasts.length > 0 && (
        <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
          {toasts.map(toast => (
            <div
              key={toast.id}
              className={[
                'animate-in slide-in-from-right rounded-lg border px-3 py-2 text-xs shadow-lg backdrop-blur-sm',
                toast.type === 'error'
                  ? 'border-red-400/20 bg-red-500/10 text-red-200'
                  : toast.type === 'warn'
                    ? 'border-amber-400/20 bg-amber-500/10 text-amber-200'
                    : 'border-emerald-400/20 bg-emerald-500/10 text-emerald-200',
              ].join(' ')}
            >
              {toast.message}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/* ── Welcome Tab ── */

function WelcomeTab({ onOpenLibrary }: { onOpenLibrary: () => void }) {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [updateInfo, setUpdateInfo] = useState<{ latestVersion?: string; updateURL?: string } | null>(null)

  useEffect(() => {
    let active = true
    void fetchDashboardStats()
      .then((next) => {
        if (active) {
          setStats(next)
        }
      })
      .catch(() => {
        if (active) {
          setStats(null)
        }
      })
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    let active = true
    void fetch('/app/version')
      .then(r => r.json())
      .then(data => {
        if (active && data.latestVersion) {
          setUpdateInfo({ latestVersion: data.latestVersion, updateURL: data.updateURL })
        }
      })
      .catch(() => {})
    return () => { active = false }
  }, [])

  return (
    <div data-testid="welcome-scroll" className="h-full overflow-y-auto px-5 pb-6 pt-4">
      <div className="mx-auto flex min-h-full w-full max-w-[560px] items-start justify-center">
        <div className="w-full rounded-[24px] border border-white/6 bg-[linear-gradient(180deg,rgba(255,255,255,0.04),rgba(255,255,255,0.02))] p-6 shadow-[0_20px_60px_rgba(0,0,0,0.24)]">
          <div className="flex items-center gap-3">
            <img
              src="/speechkit-icon.png"
              alt="SpeechKit"
              className="h-10 w-10 rounded-2xl bg-white/[0.04] p-1.5"
            />
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.22em] text-white/30">
                Welcome
              </p>
              <h1 className="mt-1 text-[24px] font-semibold tracking-tight text-white">
                Welcome to SpeechKit
              </h1>
            </div>
          </div>

          {updateInfo && (
            <div className="mt-3 flex items-center gap-2 rounded-lg border border-orange-400/20 bg-orange-500/10 px-3 py-2 text-xs text-orange-200">
              <span>Update available: v{updateInfo.latestVersion}</span>
              <a
                href={updateInfo.updateURL}
                target="_blank"
                rel="noopener noreferrer"
                className="ml-auto rounded-full bg-orange-500/20 px-2.5 py-1 font-medium text-orange-100 transition-colors hover:bg-orange-500/30"
              >
                Download
              </a>
            </div>
          )}

          <p className="mt-4 max-w-[46ch] text-sm leading-6 text-white/60">
            SpeechKit stays close to the edge of your screen, keeps quick notes nearby, and lets
            you move from a short thought to a full dictation without opening a heavy dashboard.
          </p>

          <div
            data-testid="welcome-kpis"
            className="mt-5 flex flex-nowrap items-center gap-3 overflow-x-auto pb-1"
          >
            <InlineStat label="Transcriptions" value={formatStatNumber(stats?.transcriptions)} />
            <InlineStat label="Quick Notes" value={formatStatNumber(stats?.quickNotes)} />
            <InlineStat label="Average WPM" value={formatAverageWPM(stats?.averageWordsPerMinute)} />
            <InlineStat label="Recorded Minutes" value={formatRecordedMinutes(stats?.totalAudioDurationMs)} />
          </div>

          <div className="mt-7">
            <h2 className="text-[10px] font-semibold uppercase tracking-[0.2em] text-white/30">
              Quick Start
            </h2>
            <div className="mt-3 grid gap-3">
              <QuickStartStep
                number="01"
                title="Hold Windows Alt to talk"
                icon={<QuickStartWaveIcon />}
              >
                Start dictation anywhere, keep speaking naturally, then release when the line is
                done.
              </QuickStartStep>
              <QuickStartStep
                number="02"
                title="Hover over the pill"
                icon={<QuickStartPillIcon />}
              >
                Create a quick note from the hover menu, speak directly into capture, or just
                speak right away and let SpeechKit paste into the active app.
              </QuickStartStep>
              <QuickStartStep
                number="03"
                title="Say Summarize on selected text"
                icon={<QuickStartSparkIcon />}
              >
                Quick words can trigger focused actions on the current selection, for example
                turning a long paragraph into a short summary.
              </QuickStartStep>
            </div>
          </div>

          <div className="mt-7">
            <h2 className="text-[10px] font-semibold uppercase tracking-[0.2em] text-white/30">
              Help & Feedback
            </h2>
            <div className="mt-3 flex flex-wrap gap-2">
              <a
                href="https://github.com/kombifyio/SpeechKit/issues"
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-full border border-white/10 bg-white/[0.03] px-4 py-2 text-xs font-medium text-white/70 transition-colors hover:bg-white/[0.06]"
              >
                Report Issue
              </a>
              <a
                href="https://github.com/kombifyio/SpeechKit/discussions"
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-full border border-white/10 bg-white/[0.03] px-4 py-2 text-xs font-medium text-white/70 transition-colors hover:bg-white/[0.06]"
              >
                Discussions
              </a>
            </div>
          </div>

          <div className="mt-6 flex flex-wrap gap-2">
            <button
              type="button"
              onClick={onOpenLibrary}
              className="rounded-full bg-orange-500/18 px-4 py-2 text-xs font-medium text-orange-100 transition-colors hover:bg-orange-500/28"
            >
              Open Library
            </button>
            <button
              type="button"
              onClick={() => {
                void fetch('/quicknotes/open-capture', { method: 'POST' })
              }}
              className="rounded-full border border-white/10 bg-white/[0.03] px-4 py-2 text-xs font-medium text-white/70 transition-colors hover:bg-white/[0.06]"
            >
              Quick Note
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function QuickStartStep({
  number,
  title,
  icon,
  children,
}: {
  number: string
  title: string
  icon: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex items-start gap-3 rounded-[20px] border border-white/6 bg-black/18 px-4 py-3">
      <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.03] text-white/72">
        {icon}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-[10px] font-semibold tracking-[0.18em] text-orange-300/55">
            {number}
          </span>
          <p className="text-sm font-medium text-white/84">{title}</p>
        </div>
        <p className="mt-1 text-xs leading-6 text-white/46">{children}</p>
      </div>
    </div>
  )
}

function InlineStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[108px] rounded-full border border-white/6 bg-white/[0.025] px-3 py-2">
      <p className="text-[9px] font-semibold uppercase tracking-[0.18em] text-white/28">
        {label}
      </p>
      <p className="mt-1 text-base font-semibold tracking-tight text-white/88">{value}</p>
    </div>
  )
}

/* ── Library Tab ── */

function LibraryTab() {
  const [history, setHistory] = useState<TranscriptionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const copyTimer = useRef<number | null>(null)
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([])
  const [copiedNote, setCopiedNote] = useState<number | null>(null)
  const sortedHistory = useMemo(() => sortByNewest(history, (record) => record.createdAt), [history])
  const sortedQuickNotes = useMemo(() => sortByNewest(quickNotes, (note) => note.createdAt), [quickNotes])
  const pinnedQuickNotes = useMemo(
    () => sortedQuickNotes.filter((note) => note.pinned),
    [sortedQuickNotes],
  )
  const recentQuickNotes = useMemo(
    () => sortedQuickNotes.filter((note) => !note.pinned),
    [sortedQuickNotes],
  )

  useEffect(() => {
    let active = true
    void fetchHistory()
      .then((records) => {
        if (!active) return
        setHistory(records)
        setLoading(false)
      })
      .catch(() => {
        if (!active) return
        setLoading(false)
      })
    void fetchQuickNotes()
      .then((notes) => {
        if (active) setQuickNotes(notes)
      })
      .catch(() => {})
    return () => {
      active = false
      if (copyTimer.current) window.clearTimeout(copyTimer.current)
    }
  }, [])

  const copyText = useCallback((id: number, text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopiedId(id)
      if (copyTimer.current) window.clearTimeout(copyTimer.current)
      copyTimer.current = window.setTimeout(() => setCopiedId(null), 1200)
    })
  }, [])

  const handlePinNote = async (id: number, pinned: boolean) => {
    try {
      await pinQuickNote(id, pinned)
      const notes = await fetchQuickNotes()
      setQuickNotes(notes)
    } catch {
      return
    }
  }

  const handleDeleteNote = async (id: number) => {
    try {
      await deleteQuickNote(id)
      const notes = await fetchQuickNotes()
      setQuickNotes(notes)
    } catch {
      return
    }
  }

  const handleCopyNote = (id: number, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedNote(id)
    setTimeout(() => setCopiedNote(null), 1200)
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex flex-shrink-0 items-center gap-3 px-5 pt-4 pb-3">
        <img
          src="/speechkit-icon.png"
          alt="SpeechKit"
          className="h-8 w-8 rounded-lg"
        />
        <div>
          <h1 className="text-sm font-semibold tracking-tight text-white">
            Library
          </h1>
          <p className="text-[11px] text-white/40">
            Transcriptions and quick notes, sorted by date
          </p>
        </div>
      </div>

      {/* Two-column layout */}
      <div className="flex min-h-0 flex-1 gap-3 px-5 pb-4">
        {/* Left: Transcriptions */}
        <div className="flex min-h-0 flex-1 flex-col">
          <span className="mb-2 flex-shrink-0 text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35">
            Recent Transcriptions
          </span>
          <div className="flex-1 overflow-y-auto rounded-lg">
            {loading && (
              <p className="py-4 text-center text-xs text-white/30">Loading...</p>
            )}
            {!loading && sortedHistory.length === 0 && (
              <p className="py-8 text-center text-xs text-white/30">
                No transcriptions yet. Press your hotkey to start.
              </p>
            )}
            {!loading && sortedHistory.length > 0 && (
              <div className="flex flex-col gap-1">
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
          <div className="flex flex-shrink-0 items-center justify-between mb-2">
            <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35">
              Quick Notes
            </span>
            <button
              type="button"
              onClick={() => fetch('/quicknotes/open-editor', { method: 'POST' })}
              className="rounded-md bg-orange-500/20 px-2.5 py-1 text-[10px] font-medium text-orange-200 hover:bg-orange-500/30"
            >
              + New
            </button>
          </div>

          <div className="mt-1 flex-1 overflow-y-auto rounded-lg">
            {sortedQuickNotes.length === 0 && (
              <p className="py-4 text-center text-xs text-white/25">
                No quick notes yet.
              </p>
            )}
            <div className="flex flex-col gap-1.5">
              {pinnedQuickNotes.length > 0 && (
                <>
                  <span className="mb-1 mt-0.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-orange-200/65">
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
                  {recentQuickNotes.length > 0 ? (
                    <span className="mb-1 mt-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-white/26">
                      Recent Notes
                    </span>
                  ) : null}
                </>
              )}
              {(pinnedQuickNotes.length > 0 ? recentQuickNotes : sortedQuickNotes).map((note) => (
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
      </div>
    </div>
  )
}

function QuickNoteRow({
  note,
  copied,
  onCopy,
  onDelete,
  onPin,
  onRevealAudio,
}: {
  note: QuickNote
  copied: boolean
  onCopy: (id: number, text: string) => void
  onDelete: (id: number) => void
  onPin: (id: number, pinned: boolean) => Promise<void>
  onRevealAudio: (kind: 'transcription' | 'quicknote', id: number) => Promise<string>
}) {
  return (
    <div
      data-testid="quicknote-row"
      className="group rounded-lg border border-white/6 bg-white/[0.02] px-3 py-2"
    >
      <p className="line-clamp-3 text-xs leading-relaxed text-white/70">
        {note.text}
      </p>
      <div className="mt-1.5 flex items-center gap-2">
        <span className="text-[10px] text-white/25">
          {formatLibraryTimestamp(note.createdAt)}
        </span>
        {note.pinned ? (
          <span className="rounded bg-orange-500/12 px-1.5 py-0.5 text-[10px] text-orange-200/90">
            Pinned
          </span>
        ) : null}
        {note.provider && note.provider !== 'manual' && (
          <span className="rounded bg-white/6 px-1.5 py-0.5 text-[10px] text-white/30">
            {note.provider}
          </span>
        )}
        {note.audio ? (
          <>
            <span className="rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
              {formatAudioDuration(note.audio.durationMs)}
            </span>
          </>
        ) : null}
        <div className="ml-auto flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
          {note.audio ? (
            <>
              <a
                href={dashboardAudioDownloadURL('quicknote', note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-white/50 hover:bg-white/5 hover:text-white/80"
                aria-label="Download audio"
              >
                Download audio
              </a>
              <button
                type="button"
                onClick={() => void onRevealAudio('quicknote', note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-white/40 hover:bg-white/5 hover:text-white/70"
                aria-label="Show file"
              >
                Show file
              </button>
            </>
          ) : null}
          <button
            type="button"
            onClick={() => void onPin(note.id, !note.pinned)}
            className={`rounded px-1.5 py-0.5 text-[10px] ${
              note.pinned
                ? 'text-orange-400 hover:bg-orange-500/10'
                : 'text-white/40 hover:bg-white/5 hover:text-white/70'
            }`}
          >
            {note.pinned ? 'Unpin' : 'Pin'}
          </button>
          <button
            type="button"
            onClick={() =>
              fetch(`/quicknotes/open-editor?id=${note.id}`, { method: 'POST' })
            }
            className="rounded px-1.5 py-0.5 text-[10px] text-orange-300/60 hover:bg-orange-500/10 hover:text-orange-300"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => onCopy(note.id, note.text)}
            className="rounded px-1.5 py-0.5 text-[10px] text-white/40 hover:bg-white/5 hover:text-white/70"
          >
            {copied ? 'Copied!' : 'Copy'}
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
  )
}

function QuickStartWaveIcon() {
  return (
    <svg viewBox="0 0 40 40" className="h-6 w-6" fill="none">
      <path d="M7 21h4M15 16v10M22 13v14M29 17v6" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
      <rect x="4.5" y="8.5" width="31" height="23" rx="11.5" stroke="currentColor" strokeOpacity="0.22" />
    </svg>
  )
}

function QuickStartPillIcon() {
  return (
    <svg viewBox="0 0 40 40" className="h-6 w-6" fill="none">
      <rect x="5" y="14" width="30" height="12" rx="6" fill="currentColor" fillOpacity="0.18" stroke="currentColor" strokeOpacity="0.35" />
      <circle cx="13" cy="20" r="2" fill="currentColor" />
      <circle cx="20" cy="20" r="2" fill="currentColor" fillOpacity="0.7" />
      <circle cx="27" cy="20" r="2" fill="currentColor" fillOpacity="0.45" />
    </svg>
  )
}

function QuickStartSparkIcon() {
  return (
    <svg viewBox="0 0 40 40" className="h-6 w-6" fill="none">
      <rect x="7" y="8" width="18" height="24" rx="4" stroke="currentColor" strokeOpacity="0.28" />
      <path d="M13 15h8M13 20h6M13 25h5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <path d="M29 10l1.8 3.7L34.5 15l-3.7 1.3L29 20l-1.8-3.7L23.5 15l3.7-1.3L29 10Z" fill="currentColor" />
    </svg>
  )
}

function TranscriptionRow({
  record,
  copied,
  onCopy,
  onRevealAudio,
}: {
  record: TranscriptionRecord
  copied: boolean
  onCopy: (id: number, text: string) => void
  onRevealAudio: (kind: 'transcription' | 'quicknote', id: number) => Promise<string>
}) {
  return (
    <div data-testid="transcription-row" className="group flex items-start gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-white/[0.03]">
      {/* Text content */}
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm leading-snug text-white/80">
          {record.text}
        </p>
        <div className="mt-1 flex items-center gap-1.5 overflow-hidden">
          <span className="shrink-0 rounded bg-white/6 px-1.5 py-0.5 text-[10px] font-medium text-white/40">
            {record.provider}
          </span>
          {record.model ? (
            <span className="shrink-0 truncate rounded bg-sky-500/12 px-1.5 py-0.5 text-[10px] text-sky-200/90">
              {formatTranscriptionModelLabel(record.model)}
            </span>
          ) : null}
          {record.audio ? (
            <span className="shrink-0 rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
              {formatAudioDuration(record.audio.durationMs)}
            </span>
          ) : null}
          <span className="shrink-0 text-[11px] text-white/25">
            {formatLibraryTimestamp(record.createdAt)}
          </span>
        </div>
      </div>

      <div className="mt-0.5 flex flex-shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {record.audio ? (
          <>
            <a
              href={dashboardAudioDownloadURL('transcription', record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-white/50 hover:bg-white/5 hover:text-white/80"
              aria-label="Download audio"
            >
              Download audio
            </a>
            <button
              type="button"
              onClick={() => void onRevealAudio('transcription', record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-white/40 hover:bg-white/5 hover:text-white/70"
              aria-label="Show file"
            >
              Show file
            </button>
          </>
        ) : null}
        <button
          type="button"
          onClick={() => onCopy(record.id, record.text)}
          className="flex h-7 w-7 items-center justify-center rounded-md text-white/20 transition-colors hover:bg-white/10 hover:text-white/60"
          title="Copy to clipboard"
        >
          {copied ? (
            <span className="text-[10px] font-medium text-emerald-400">Copied!</span>
          ) : (
            <ClipboardIcon />
          )}
        </button>
      </div>
    </div>
  )
}

function formatAudioDuration(durationMs: number) {
  const seconds = durationMs / 1000
  if (seconds >= 60) {
    return `${(seconds / 60).toFixed(1)}m`
  }
  return `${seconds.toFixed(1)}s`
}

function formatTranscriptionModelLabel(model: string) {
  const normalized = model.trim()
  if (!normalized) {
    return ''
  }
  if (normalized.endsWith('whisper-large-v3-turbo')) {
    return 'turbo-v3'
  }
  if (normalized.endsWith('whisper-large-v3')) {
    return 'large-v3'
  }

  const leaf = normalized.split(/[\\/]/).pop() ?? normalized
  return leaf.replace(/\.(bin|gguf|onnx)$/i, '')
}

/* ── Logs Tab ── */

function LogsTab() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)
  const pollRef = useRef<number | null>(null)

  const loadLogs = useCallback(async () => {
    try {
      return await fetchLogs()
    } catch {
      return null
    }
  }, [])

  useEffect(() => {
    let active = true
    const syncLogs = async () => {
      const entries = await loadLogs()
      if (!active) {
        return
      }
      if (entries) {
        setLogs(entries)
      }
      setLoading(false)
    }

    void syncLogs()
    pollRef.current = window.setInterval(() => {
      void syncLogs()
    }, 2000)
    return () => {
      active = false
      if (pollRef.current) window.clearInterval(pollRef.current)
    }
  }, [loadLogs])

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [logs])

  return (
    <div className="flex h-full flex-col px-5 pt-4 pb-4">
      <span className="mb-2 flex-shrink-0 text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35">
        Application Logs
      </span>

      <div
        ref={containerRef}
        className="flex-1 overflow-y-auto rounded-lg bg-white/[0.02] p-3 font-mono text-xs leading-relaxed"
      >
        {loading && (
          <p className="text-white/30">Loading logs...</p>
        )}

        {!loading && logs.length === 0 && (
          <p className="text-white/30">No log entries.</p>
        )}

        {logs.map((entry, i) => (
          <div key={i} className="flex gap-2">
            <span className="flex-shrink-0 text-white/20">
              {formatLogTime(entry.timestamp)}
            </span>
            <span className={logColor(entry.type)}>
              {entry.message}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

/* ── Setup Wizard ── */

type WizardStep = 'welcome' | 'microphone' | 'hotkey' | 'done'

function SetupWizard({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState<WizardStep>('welcome')
  const [devices, setDevices] = useState<AudioDevice[]>([])
  const [selectedDevice, setSelectedDevice] = useState('')
  const [hotkey, setHotkey] = useState('win+alt')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    void fetchAudioDevices()
      .then(res => {
        setDevices(res.devices)
        setSelectedDevice(res.selectedDeviceId || res.devices[0]?.deviceId || '')
      })
      .catch(() => {})
  }, [])

  const handleDeviceSelect = (deviceId: string) => {
    setSelectedDevice(deviceId)
    void setAudioDevice(deviceId).catch(() => {})
  }

  const handleFinish = async () => {
    setLoading(true)
    try {
      const body = new URLSearchParams()
      body.set('dictate_hotkey', hotkey)
      body.set('audio_device_id', selectedDevice)
      await fetch('/settings/update', { method: 'POST', body })
    } catch { /* ignore */ }
    onComplete()
  }

  return (
    <div className="flex h-screen flex-col items-center justify-center bg-[#0b0f14] text-[13px] text-white/90 px-6">
      <div className="w-full max-w-[480px] rounded-[24px] border border-white/6 bg-[linear-gradient(180deg,rgba(255,255,255,0.04),rgba(255,255,255,0.02))] p-8 shadow-[0_20px_60px_rgba(0,0,0,0.24)]">

        {/* Progress dots */}
        <div className="mb-6 flex justify-center gap-2">
          {(['welcome', 'microphone', 'hotkey', 'done'] as WizardStep[]).map(s => (
            <div
              key={s}
              className={[
                'h-1.5 w-6 rounded-full transition-colors',
                s === step ? 'bg-orange-500' : 'bg-white/10',
              ].join(' ')}
            />
          ))}
        </div>

        {step === 'welcome' && (
          <>
            <div className="flex items-center gap-3">
              <img src="/speechkit-icon.png" alt="SpeechKit" className="h-12 w-12 rounded-2xl bg-white/[0.04] p-2" />
              <div>
                <h1 className="text-xl font-semibold tracking-tight text-white">Welcome to SpeechKit</h1>
                <p className="text-xs text-white/40">Let's get you set up in a few quick steps.</p>
              </div>
            </div>
            <div className="mt-6 space-y-3">
              <SetupFeatureRow title="Dictation" desc="Push-to-talk transcription in any app" />
              <SetupFeatureRow title="Assist" desc="Smart voice commands with AI responses" />
              <SetupFeatureRow title="Voice Agent" desc="Real-time audio conversations (Coming soon)" />
            </div>
            <button
              type="button"
              onClick={() => setStep('microphone')}
              className="mt-8 w-full rounded-xl bg-orange-500/20 py-2.5 text-sm font-medium text-orange-100 transition-colors hover:bg-orange-500/30"
            >
              Get Started
            </button>
          </>
        )}

        {step === 'microphone' && (
          <>
            <h2 className="text-lg font-semibold text-white">Select Your Microphone</h2>
            <p className="mt-1 text-xs text-white/40">Choose the microphone SpeechKit should use for recording.</p>
            <div className="mt-5 space-y-1.5">
              {devices.length === 0 && (
                <p className="text-xs text-white/30">No microphones detected.</p>
              )}
              {devices.map(device => (
                <button
                  key={device.deviceId}
                  type="button"
                  onClick={() => handleDeviceSelect(device.deviceId)}
                  className={[
                    'w-full rounded-lg border px-3 py-2.5 text-left text-xs transition-colors',
                    selectedDevice === device.deviceId
                      ? 'border-orange-500/40 bg-orange-500/10 text-orange-100'
                      : 'border-white/8 bg-white/[0.02] text-white/60 hover:bg-white/[0.04]',
                  ].join(' ')}
                >
                  {device.label}
                  {device.isDefault && <span className="ml-2 text-[10px] text-white/25">(Default)</span>}
                </button>
              ))}
            </div>
            <div className="mt-6 flex gap-2">
              <button type="button" onClick={() => setStep('welcome')} className="flex-1 rounded-xl border border-white/10 bg-white/[0.03] py-2.5 text-sm font-medium text-white/60 transition-colors hover:bg-white/[0.06]">
                Back
              </button>
              <button type="button" onClick={() => setStep('hotkey')} className="flex-1 rounded-xl bg-orange-500/20 py-2.5 text-sm font-medium text-orange-100 transition-colors hover:bg-orange-500/30">
                Continue
              </button>
            </div>
          </>
        )}

        {step === 'hotkey' && (
          <>
            <h2 className="text-lg font-semibold text-white">Choose Your Hotkey</h2>
            <p className="mt-1 text-xs text-white/40">This is the push-to-talk key for dictation. Hold it to speak, release to stop.</p>
            <div className="mt-5 flex flex-wrap gap-2">
              {[
                { value: 'win+alt', label: 'Win + Alt' },
                { value: 'ctrl+win', label: 'Ctrl + Win' },
                { value: 'ctrl+shift+d', label: 'Ctrl + Shift + D' },
                { value: 'ctrl+shift+k', label: 'Ctrl + Shift + K' },
              ].map(opt => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setHotkey(opt.value)}
                  className={[
                    'rounded-lg border px-4 py-2.5 text-xs font-medium transition-colors',
                    hotkey === opt.value
                      ? 'border-orange-500/40 bg-orange-500/10 text-orange-100'
                      : 'border-white/8 bg-white/[0.02] text-white/60 hover:bg-white/[0.04]',
                  ].join(' ')}
                >
                  {opt.label}
                </button>
              ))}
            </div>
            <div className="mt-6 flex gap-2">
              <button type="button" onClick={() => setStep('microphone')} className="flex-1 rounded-xl border border-white/10 bg-white/[0.03] py-2.5 text-sm font-medium text-white/60 transition-colors hover:bg-white/[0.06]">
                Back
              </button>
              <button type="button" onClick={() => setStep('done')} className="flex-1 rounded-xl bg-orange-500/20 py-2.5 text-sm font-medium text-orange-100 transition-colors hover:bg-orange-500/30">
                Continue
              </button>
            </div>
          </>
        )}

        {step === 'done' && (
          <>
            <div className="text-center">
              <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-emerald-500/10">
                <svg className="h-8 w-8 text-emerald-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                </svg>
              </div>
              <h2 className="mt-4 text-lg font-semibold text-white">You're All Set</h2>
              <p className="mt-2 text-xs leading-5 text-white/40">
                Hold <span className="rounded bg-white/10 px-1.5 py-0.5 font-mono text-white/70">{hotkey.replace(/\+/g, ' + ').replace('win', 'Win').replace('alt', 'Alt').replace('ctrl', 'Ctrl').replace('shift', 'Shift')}</span> to start dictating.
                SpeechKit will transcribe your speech and paste it into the active app.
              </p>
            </div>
            <button
              type="button"
              onClick={() => void handleFinish()}
              disabled={loading}
              className="mt-8 w-full rounded-xl bg-orange-500/20 py-2.5 text-sm font-medium text-orange-100 transition-colors hover:bg-orange-500/30 disabled:opacity-50"
            >
              {loading ? 'Saving...' : 'Open Dashboard'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}

function SetupFeatureRow({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="flex items-start gap-3 rounded-lg bg-white/[0.02] px-3 py-2.5">
      <div className="mt-0.5 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-orange-500/60" />
      <div>
        <p className="text-xs font-medium text-white/80">{title}</p>
        <p className="text-[11px] text-white/35">{desc}</p>
      </div>
    </div>
  )
}

/* ── Shared Primitives ── */

function TabBtn({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
        active
          ? 'bg-white/10 text-white'
          : 'text-white/40 hover:text-white/60',
      ].join(' ')}
    >
      {children}
    </button>
  )
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
  )
}

/* ── Utilities ── */

function sortByNewest<T>(items: T[], getDate: (item: T) => string): T[] {
  return [...items].sort((a, b) => new Date(getDate(b)).getTime() - new Date(getDate(a)).getTime())
}

function resolveInitialDashboardTab(): Tab {
  if (typeof window === 'undefined') {
    return 'welcome'
  }

  const hashTab = parseDashboardTab(window.location.hash)
  if (hashTab) {
    return hashTab
  }

  const storedTab = parseDashboardTab(window.sessionStorage.getItem(DASHBOARD_TAB_STORAGE_KEY) ?? '')
  if (storedTab) {
    return storedTab
  }

  return 'welcome'
}

function parseDashboardTab(value: string): Tab | null {
  const normalized = value.replace(/^#/, '').trim().toLowerCase()
  switch (normalized) {
    case 'welcome':
    case 'library':
    case 'settings':
    case 'logs':
      return normalized
    default:
      return null
  }
}

function formatLibraryTimestamp(iso: string): string {
  try {
    const d = new Date(iso)
    const date = new Intl.DateTimeFormat('en-GB', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
    }).format(d)
    const time = new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }).format(d)
    return `${date} · ${time}`
  } catch {
    return ''
  }
}

function formatStatNumber(value?: number): string {
  if (typeof value !== 'number') return '--'
  return new Intl.NumberFormat('en-GB').format(value)
}

function formatAverageWPM(value?: number): string {
  if (typeof value !== 'number' || Number.isNaN(value) || value <= 0) {
    return '--'
  }
  return value.toFixed(1)
}

function formatRecordedMinutes(durationMs?: number): string {
  if (typeof durationMs !== 'number' || durationMs <= 0) {
    return '--'
  }
  return (durationMs / 60000).toFixed(1)
}

function formatLogTime(iso: string): string {
  try {
    const d = new Date(iso)
    return d.toLocaleTimeString('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return ''
  }
}

function logColor(type: string): string {
  switch (type) {
    case 'error':
      return 'text-red-400'
    case 'warn':
      return 'text-yellow-400'
    case 'success':
      return 'text-green-400'
    default:
      return 'text-white/50'
  }
}

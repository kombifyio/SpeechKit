import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

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

type Tab = 'dashboard' | 'library' | 'settings' | 'logs'

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
            if (log.type === 'error') addToast(log.message, 'error')
          }
          lastLogCountRef.current = logs.length
        }
      } catch { /* ignore */ }
    }, 3000)
    return () => clearInterval(interval)
  }, [addToast])

  useEffect(() => {
    if (typeof window === 'undefined') return
    window.sessionStorage.setItem(DASHBOARD_TAB_STORAGE_KEY, tab)
    const nextURL = new URL(window.location.href)
    nextURL.hash = tab === 'dashboard' ? '' : `#${tab}`
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
    return <div className="flex h-screen items-center justify-center bg-[#131318] text-[#938ea1] text-sm">Loading...</div>
  }

  return (
    <div className="flex h-screen bg-[#131318] text-[13px] text-[#e4e1e9]">
      {/* Sidebar */}
      <aside className="flex w-55 shrink-0 flex-col border-r border-[#35343a]/20 bg-[#1f1f25] py-6 px-4">
        <div className="mb-8 px-2">
          <span className="text-xl font-bold tracking-tighter text-[#cabeff]">SpeechKit</span>
          <span className="block text-[10px] uppercase tracking-widest text-[#938ea1]/60 mt-0.5">v0.16.0</span>
        </div>
        <nav className="flex-1 space-y-1">
          <NavBtn active={tab === 'dashboard'} onClick={() => setTab('dashboard')}>
            <NavIcon d="M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z" />
            Dashboard
          </NavBtn>
          <NavBtn active={tab === 'library'} onClick={() => setTab('library')}>
            <NavIcon d="M20 6h-8l-2-2H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z" />
            Library
          </NavBtn>
          <NavBtn active={tab === 'settings'} onClick={() => setTab('settings')}>
            <NavIcon d="M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.49.49 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54a.484.484 0 0 0-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96a.49.49 0 0 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.07.62-.07.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z" />
            Settings
          </NavBtn>
          <NavBtn active={tab === 'logs'} onClick={() => setTab('logs')}>
            <NavIcon d="M20 19.59V8l-6-6H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c.45 0 .85-.15 1.19-.41l-4.43-4.43c-.8.52-1.74.84-2.76.84-2.76 0-5-2.24-5-5s2.24-5 5-5 5 2.24 5 5c0 1.02-.31 1.96-.84 2.76L20 19.59z" />
            Logs
          </NavBtn>
        </nav>
      </aside>

      {/* Main content */}
      <main className="flex-1 flex flex-col min-w-0 overflow-hidden">
        <div className="flex-1 overflow-hidden">
          {tab === 'dashboard' && <DashboardView onOpenLibrary={() => setTab('library')} onOpenSettings={() => setTab('settings')} />}
          {tab === 'library' && <LibraryView />}
          {tab === 'settings' && (
            <div className="h-full overflow-y-auto">
              <SettingsApp />
            </div>
          )}
          {tab === 'logs' && <LogsView />}
        </div>
      </main>

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

/* ── Dashboard View ── */

function DashboardView({
  onOpenLibrary,
  onOpenSettings,
}: {
  onOpenLibrary: () => void
  onOpenSettings: () => void
}) {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [updateInfo, setUpdateInfo] = useState<{ latestVersion?: string; updateURL?: string } | null>(null)
  const [history, setHistory] = useState<TranscriptionRecord[]>([])
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([])

  useEffect(() => {
    let active = true
    void fetchDashboardStats()
      .then(next => { if (active) setStats(next) })
      .catch(() => { if (active) setStats(null) })
    return () => { active = false }
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

  useEffect(() => {
    let active = true
    void fetchHistory()
      .then(records => { if (active) setHistory(records) })
      .catch(() => { if (active) setHistory([]) })
    void fetchQuickNotes()
      .then(notes => { if (active) setQuickNotes(notes) })
      .catch(() => { if (active) setQuickNotes([]) })
    return () => { active = false }
  }, [])

  const sortedHistory = useMemo(() => sortByNewest(history, r => r.createdAt), [history])
  const sortedQuickNotes = useMemo(() => sortByNewest(quickNotes, n => n.createdAt), [quickNotes])
  const latestTranscription = sortedHistory[0] ?? null
  const pinnedNotes = sortedQuickNotes.filter(n => n.pinned)
  const featuredNotes = pinnedNotes.length > 0 ? pinnedNotes.slice(0, 3) : sortedQuickNotes.slice(0, 3)

  return (
    <div data-testid="welcome-scroll" className="h-full overflow-y-auto">
      {/* Header */}
      <header className="flex items-center justify-between px-8 h-16 border-b border-[#35343a]/20 bg-[#131318]/80 backdrop-blur-xl">
        <h2 className="text-[#cabeff] text-sm font-semibold">Dashboard</h2>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => void fetch('/quicknotes/open-capture', { method: 'POST' })}
            className="rounded-full border border-[#484555]/50 bg-[#1f1f25] px-4 py-1.5 text-xs font-medium text-[#c9c4d8] hover:bg-[#2a292f] transition-colors"
          >
            Quick Note
          </button>
        </div>
      </header>

      <div className="p-8 space-y-8 pb-12">
        {/* Update banner */}
        {updateInfo && (
          <div className="flex items-center gap-2 rounded-xl border border-[#cabeff]/20 bg-[#cabeff]/5 px-4 py-3 text-xs text-[#cabeff]">
            <span>Update available: v{updateInfo.latestVersion}</span>
            <a
              href={updateInfo.updateURL}
              target="_blank"
              rel="noopener noreferrer"
              className="ml-auto rounded-full bg-[#cabeff]/20 px-3 py-1 font-medium text-[#cabeff] hover:bg-[#cabeff]/30 transition-colors"
            >
              Download
            </a>
          </div>
        )}

        {/* KPI Row */}
        <div data-testid="welcome-kpis" className="grid grid-cols-4 gap-4">
          <KPICard label="Total Recordings" value={formatStatNumber(stats?.transcriptions)} />
          <KPICard label="Average WPM" value={formatAverageWPM(stats?.averageWordsPerMinute)} />
          <KPICard label="Total Words" value={formatStatNumber(stats?.totalWords)} />
          <KPICard label="Recorded Minutes" value={formatRecordedMinutes(stats?.totalAudioDurationMs)} />
        </div>

        {/* Recent activity: transcriptions + notes */}
        {(latestTranscription || featuredNotes.length > 0) ? (
          <div>
            <h3 className="text-xs font-bold uppercase tracking-widest text-[#938ea1] mb-4">Recent Activity</h3>
            <div className="grid gap-6 md:grid-cols-[1.4fr_1fr]">
            {/* Latest transcription */}
            <section className="rounded-xl bg-[#1f1f25] p-6">
              <p className="text-[10px] font-bold text-[#938ea1] uppercase tracking-widest mb-1">
                Latest transcription
              </p>
              <h3 className="text-lg font-semibold text-[#e4e1e9]">Latest capture</h3>
              {latestTranscription ? (
                <>
                  <p className="mt-4 text-sm leading-7 text-[#e4e1e9]/85">
                    {latestTranscription.text}
                  </p>
                  <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-[#938ea1]">
                    <span>{formatLibraryTimestamp(latestTranscription.createdAt)}</span>
                    <span className="rounded bg-[#cabeff]/12 px-1.5 py-0.5 text-[#cabeff]">
                      {latestTranscription.provider}
                    </span>
                    {latestTranscription.model && (
                      <span className="rounded bg-[#0e0e13] px-1.5 py-0.5 text-[#c9c4d8]">
                        {formatTranscriptionModelLabel(latestTranscription.model)}
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <p className="mt-4 text-sm text-[#938ea1]">No transcriptions yet.</p>
              )}
            </section>

            {/* Quick notes */}
            <section className="rounded-xl bg-[#1f1f25] p-6">
              <p className="text-[10px] font-bold text-[#938ea1] uppercase tracking-widest mb-1">
                Pinned notes
              </p>
              <h3 className="text-lg font-semibold text-[#e4e1e9]">Fast recall</h3>
              <div className="mt-4 flex flex-col gap-2">
                {featuredNotes.map(note => (
                  <div key={note.id} className="rounded-lg bg-[#0e0e13] px-4 py-3">
                    <p className="line-clamp-3 text-sm leading-6 text-[#e4e1e9]/80">{note.text}</p>
                    <div className="mt-2 flex items-center gap-2 text-[10px] text-[#938ea1]">
                      <span>{formatLibraryTimestamp(note.createdAt)}</span>
                      {note.pinned && (
                        <span className="rounded bg-[#cabeff]/12 px-1.5 py-0.5 text-[#cabeff]">Pinned</span>
                      )}
                    </div>
                  </div>
                ))}
                {featuredNotes.length === 0 && (
                  <p className="text-sm text-[#938ea1]">
                    Create a quick note to keep names, snippets, or follow-ups close.
                  </p>
                )}
              </div>
            </section>
          </div>
          </div>
        ) : (
          /* Empty state / Quick Start */
          <div className="rounded-xl bg-[#1f1f25] p-8">
            <h3 className="text-xl font-semibold text-[#e4e1e9]">Welcome to SpeechKit</h3>
            <p className="mt-2 text-sm text-[#938ea1] max-w-[50ch]">
              SpeechKit stays close to the edge of your screen, keeps quick notes nearby, and lets
              you move from a short thought to a full dictation without opening a heavy dashboard.
            </p>

            <div className="mt-7">
              <h4 className="text-[10px] font-bold uppercase tracking-widest text-[#938ea1]">
                Quick Start
              </h4>
              <div className="mt-3 grid gap-3">
                <QuickStartCard number="01" title="Hold Windows Alt to talk">
                  Start dictation anywhere, keep speaking naturally, then release when done.
                </QuickStartCard>
                <QuickStartCard number="02" title="Hover over the pill">
                  Create a quick note from the hover menu, or speak directly into capture.
                </QuickStartCard>
                <QuickStartCard number="03" title="Say Summarize on selected text">
                  Quick words trigger focused actions on the current selection.
                </QuickStartCard>
              </div>
            </div>

            <div className="mt-6 flex flex-wrap gap-2">
              <button
                type="button"
                onClick={onOpenLibrary}
                className="rounded-full signature-gradient px-5 py-2 text-xs font-bold text-[#2b0088] transition-all hover:opacity-90"
              >
                Open Library
              </button>
              <button
                type="button"
                onClick={onOpenSettings}
                className="rounded-full border border-[#484555]/50 bg-[#1f1f25] px-4 py-2 text-xs font-medium text-[#c9c4d8] hover:bg-[#2a292f] transition-colors"
              >
                Open Settings
              </button>
            </div>
          </div>
        )}


      </div>
    </div>
  )
}

/* ── Library View ── */

function LibraryView() {
  const [history, setHistory] = useState<TranscriptionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const copyTimer = useRef<number | null>(null)
  const [quickNotes, setQuickNotes] = useState<QuickNote[]>([])
  const [copiedNote, setCopiedNote] = useState<number | null>(null)
  const sortedHistory = useMemo(() => sortByNewest(history, r => r.createdAt), [history])
  const sortedQuickNotes = useMemo(() => sortByNewest(quickNotes, n => n.createdAt), [quickNotes])
  const pinnedQuickNotes = useMemo(() => sortedQuickNotes.filter(n => n.pinned), [sortedQuickNotes])
  const recentQuickNotes = useMemo(() => sortedQuickNotes.filter(n => !n.pinned), [sortedQuickNotes])

  useEffect(() => {
    let active = true
    void fetchHistory()
      .then(records => {
        if (!active) return
        setHistory(records)
        setLoading(false)
      })
      .catch(() => { if (!active) return; setLoading(false) })
    void fetchQuickNotes()
      .then(notes => { if (active) setQuickNotes(notes) })
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
    } catch { return }
  }

  const handleDeleteNote = async (id: number) => {
    try {
      await deleteQuickNote(id)
      const notes = await fetchQuickNotes()
      setQuickNotes(notes)
    } catch { return }
  }

  const handleCopyNote = (id: number, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedNote(id)
    setTimeout(() => setCopiedNote(null), 1200)
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <header className="flex items-center justify-between px-8 h-16 border-b border-[#35343a]/20 bg-[#131318]/80 backdrop-blur-xl shrink-0">
        <div>
          <h1 role="heading" className="text-sm font-semibold text-[#cabeff]">Library</h1>
          <p className="text-[11px] text-[#938ea1]">Transcriptions and quick notes</p>
        </div>
        <button
          type="button"
          onClick={() => fetch('/quicknotes/open-editor', { method: 'POST' })}
          className="rounded-full signature-gradient px-4 py-1.5 text-xs font-bold text-[#2b0088] hover:opacity-90 transition-all"
        >
          + New
        </button>
      </header>

      {/* Two-column layout */}
      <div className="flex min-h-0 flex-1 gap-4 px-8 py-6">
        {/* Left: Transcriptions */}
        <div className="flex min-h-0 flex-1 flex-col">
          <span className="mb-3 text-[10px] font-bold uppercase tracking-widest text-[#938ea1]">
            Recent Transcriptions
          </span>
          <div className="flex-1 overflow-y-auto rounded-xl bg-[#1f1f25] p-1">
            {loading && <p className="py-4 text-center text-xs text-[#938ea1]">Loading...</p>}
            {!loading && sortedHistory.length === 0 && (
              <p className="py-8 text-center text-xs text-[#938ea1]">
                No transcriptions yet. Press your hotkey to start.
              </p>
            )}
            {!loading && sortedHistory.length > 0 && (
              <div className="flex flex-col gap-0.5">
                {sortedHistory.map(record => (
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
            <span className="text-[10px] font-bold uppercase tracking-widest text-[#938ea1]">
              Quick Notes
            </span>
          </div>
          <div className="flex-1 overflow-y-auto rounded-xl bg-[#1f1f25] p-3">
            {sortedQuickNotes.length === 0 && (
              <p className="py-4 text-center text-xs text-[#938ea1]">No quick notes yet.</p>
            )}
            <div className="flex flex-col gap-1.5">
              {pinnedQuickNotes.length > 0 && (
                <>
                  <span className="mb-1 mt-0.5 text-[10px] font-bold uppercase tracking-widest text-[#cabeff]/70">
                    Pinned Notes
                  </span>
                  {pinnedQuickNotes.map(note => (
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
                    <span className="mb-1 mt-2 text-[10px] font-bold uppercase tracking-widest text-[#938ea1]/70">
                      Recent Notes
                    </span>
                  )}
                </>
              )}
              {(pinnedQuickNotes.length > 0 ? recentQuickNotes : sortedQuickNotes).map(note => (
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

/* ── Logs View ── */

function LogsView() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)

  const loadLogs = useCallback(async () => {
    try { return await fetchLogs() } catch { return null }
  }, [])

  useEffect(() => {
    let active = true
    const syncLogs = async () => {
      const entries = await loadLogs()
      if (!active) return
      if (entries) setLogs(entries)
      setLoading(false)
    }
    void syncLogs()
    const timer = window.setInterval(() => void syncLogs(), 2000)
    return () => { active = false; window.clearInterval(timer) }
  }, [loadLogs])

  useEffect(() => {
    const el = containerRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [logs])

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center px-8 h-16 border-b border-[#35343a]/20 bg-[#131318]/80 backdrop-blur-xl shrink-0">
        <h2 className="text-sm font-semibold text-[#cabeff]">Application Logs</h2>
      </header>
      <div
        ref={containerRef}
        className="flex-1 overflow-y-auto px-8 py-4 font-mono text-xs leading-relaxed bg-[#0e0e13]"
      >
        {loading && <p className="text-[#938ea1]">Loading logs...</p>}
        {!loading && logs.length === 0 && <p className="text-[#938ea1]">No log entries.</p>}
        {logs.map((entry, i) => (
          <div key={i} className="flex gap-2">
            <span className="shrink-0 text-[#938ea1]/50">{formatLogTime(entry.timestamp)}</span>
            <span className={logColor(entry.type)}>{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

/* ── Setup Wizard ── */

type WizardStep = 'welcome' | 'provider' | 'done'

function SetupWizard({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState<WizardStep>('welcome')
  const [devices, setDevices] = useState<AudioDevice[]>([])
  const [selectedDevice, setSelectedDevice] = useState('')
  const [selectedProvider, setSelectedProvider] = useState('local')
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
      body.set('dictate_hotkey', 'ctrl+shift+d')
      body.set('audio_device_id', selectedDevice)
      await fetch('/settings/update', { method: 'POST', body })
    } catch { /* ignore */ }
    onComplete()
  }

  const STEPS: WizardStep[] = ['welcome', 'provider', 'done']

  return (
    <div className="flex h-screen flex-col items-center justify-center bg-[#131318] text-[#e4e1e9] px-6 relative overflow-hidden">
      {/* Ambient glow */}
      <div className="fixed top-[-10%] left-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />
      <div className="fixed bottom-[-10%] right-[-10%] w-[40%] h-[40%] rounded-full bg-[#cabeff]/5 blur-[120px] pointer-events-none" />

      {step === 'welcome' && (
        <div className="flex flex-col items-center text-center max-w-2xl w-full z-10">
          {/* Logo */}
          <div className="w-20 h-20 bg-[#2a292f] rounded-full flex items-center justify-center ambient-glow border border-white/5 mb-12">
            <svg className="w-10 h-10 text-[#947dff]" viewBox="0 0 24 24" fill="currentColor"><path d="M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5.91-3c-.49 0-.9.36-.98.85C16.52 14.2 14.47 16 12 16s-4.52-1.8-4.93-4.15a.998.998 0 0 0-.98-.85c-.61 0-1.09.54-1 1.14.49 3 2.89 5.35 5.91 5.78V20c0 .55.45 1 1 1s1-.45 1-1v-2.08a6.993 6.993 0 0 0 5.91-5.78c.1-.6-.39-1.14-1-1.14z" /></svg>
          </div>

          <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight text-[#e4e1e9]">
            Welcome to SpeechKit
          </h1>
          <p className="mt-4 text-lg text-[#b5b3c4] font-light leading-relaxed">
            Your voice, your workflow. Dictate, assist, and automate — hands-free.
          </p>

          {/* Feature cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full max-w-4xl mt-16">
            <FeatureCard icon="mic" title="Dictation" desc="Speech-to-text anywhere" />
            <FeatureCard icon="sparkle" title="AI Assist" desc="Voice-powered AI responses" />
            <FeatureCard icon="wave" title="Voice Agent" desc="Real-time audio conversations" />
          </div>

          <div className="flex flex-col items-center space-y-4 mt-16">
            <button
              type="button"
              onClick={() => setStep('provider')}
              className="signature-gradient ambient-glow text-[#2b0088] font-bold text-lg px-12 h-14 rounded-full transition-all active:scale-95 hover:opacity-90"
            >
              Get Started
            </button>
            <button
              type="button"
              onClick={() => void handleFinish()}
              className="text-[#b5b3c4] text-sm font-medium hover:text-[#e4e1e9] transition-colors"
            >
              Skip setup
            </button>
          </div>
        </div>
      )}

      {step === 'provider' && (
        <div className="w-full max-w-140 space-y-10 z-10">
          {/* Progress dots */}
          <div className="flex justify-center items-center gap-4">
            {STEPS.map((s, i) => (
              <div key={s} className={[
                'w-3 h-3 rounded-full transition-all',
                i <= STEPS.indexOf(step) ? 'bg-[#cabeff] shadow-[0_0_8px_rgba(202,190,255,0.4)]' : 'bg-[#484555]',
              ].join(' ')} />
            ))}
          </div>

          <div className="text-center space-y-3">
            <h2 className="text-3xl font-extrabold tracking-tight">Dictation Provider</h2>
            <p className="text-[#b5b3c4] text-sm max-w-md mx-auto">
              Choose how SpeechKit transcribes your voice in <span className="text-[#cabeff] font-semibold">Dictation</span> mode. You can change this later in Settings.
            </p>
          </div>

          {/* Provider cards */}
          <div className="space-y-4">
            <ProviderCard
              selected={selectedProvider === 'local'}
              onClick={() => setSelectedProvider('local')}
              title="Local (Whisper)"
              desc="Runs offline on your device. Private and free."
              recommended
            />
            <ProviderCard
              selected={selectedProvider === 'openai'}
              onClick={() => setSelectedProvider('openai')}
              title="OpenAI Whisper API"
              desc="Fast and accurate. Requires API key."
            />
            <ProviderCard
              selected={selectedProvider === 'google'}
              onClick={() => setSelectedProvider('google')}
              title="Google Speech"
              desc="High accuracy. Requires API key."
            />
          </div>

          {/* Hint for other modes */}
          <div className="bg-[#1b1b20] rounded-xl px-5 py-4 flex items-start gap-3">
            <svg className="w-5 h-5 text-[#947dff] shrink-0 mt-0.5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z" />
            </svg>
            <p className="text-[#938ea1] text-xs leading-relaxed">
              <span className="text-[#b5b3c4] font-semibold">AI Assist</span> and <span className="text-[#b5b3c4] font-semibold">Voice Agent</span> use separate models that you can configure in <span className="text-[#cabeff]">Settings → Assist / Voice Agent</span> after setup.
            </p>
          </div>

          {/* Mic selection (compact) */}
          {devices.length > 0 && (
            <div>
              <label className="text-xs font-bold text-[#938ea1] uppercase tracking-widest mb-2 block">Microphone</label>
              <select
                value={selectedDevice}
                onChange={e => handleDeviceSelect(e.target.value)}
                className="w-full bg-[#0e0e13] border-none rounded-lg px-4 py-3 text-sm text-[#e4e1e9] focus:ring-1 focus:ring-[#cabeff]/40 appearance-none"
              >
                {devices.map(d => (
                  <option key={d.deviceId} value={d.deviceId}>
                    {d.label}{d.isDefault ? ' (Default)' : ''}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="pt-4 space-y-4">
            <button
              type="button"
              onClick={() => setStep('done')}
              className="w-full h-13 signature-gradient text-[#2b0088] font-bold rounded-full ambient-glow active:scale-[0.98] transition-all flex items-center justify-center gap-2"
            >
              Continue
            </button>
            <div className="flex justify-center">
              <button
                type="button"
                onClick={() => setStep('welcome')}
                className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
              >
                Back
              </button>
            </div>
          </div>
        </div>
      )}

      {step === 'done' && (
        <div className="flex flex-col items-center text-center max-w-4xl w-full z-10">
          {/* Progress dots */}
          <div className="flex gap-3 mb-12">
            {STEPS.map(s => (
              <div key={s} className="w-2.5 h-2.5 rounded-full bg-[#cabeff]" />
            ))}
          </div>

          {/* Success icon */}
          <div className="w-24 h-24 rounded-full signature-gradient flex items-center justify-center ambient-glow ring-8 ring-[#cabeff]/10 mb-8">
            <svg className="w-12 h-12 text-[#2b0088]" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
            </svg>
          </div>

          <h2 className="text-4xl font-extrabold tracking-tight mb-4">You're all set!</h2>
          <p className="text-lg text-[#b5b3c4] max-w-md leading-relaxed">SpeechKit is ready. Here is how to get started:</p>

          {/* Quick-start cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6 w-full mt-12 mb-12">
            <WizardFeatureCard title="Ctrl+Shift+D" desc="Start dictation anywhere" />
            <WizardFeatureCard title="Ctrl+Shift+A" desc="Activate AI Assist" />
            <WizardFeatureCard title="Speak naturally" desc="SpeechKit handles the rest" />
          </div>

          <button
            type="button"
            onClick={() => void handleFinish()}
            disabled={loading}
            className="signature-gradient text-[#2b0088] h-14 rounded-full font-bold text-lg px-12 ambient-glow hover:scale-[1.02] active:scale-[0.98] transition-all disabled:opacity-50"
          >
            {loading ? 'Setting up...' : 'Start Using SpeechKit'}
          </button>
        </div>
      )}
    </div>
  )
}

/* ── Shared Components ── */

function NavBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'flex items-center gap-3 w-full px-3 py-2.5 rounded-lg text-sm transition-all',
        active
          ? 'bg-[#35343a] text-[#cabeff] font-semibold'
          : 'text-[#938ea1] hover:text-[#e4e1e9] hover:bg-[#35343a]/50',
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function NavIcon({ d }: { d: string }) {
  return (
    <svg className="w-5 h-5 shrink-0" viewBox="0 0 24 24" fill="currentColor"><path d={d} /></svg>
  )
}

function KPICard({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-[#1f1f25] p-5 rounded-xl hover:bg-[#2a292f] transition-all">
      <p className="text-[10px] font-bold text-[#938ea1] uppercase tracking-widest mb-2">{label}</p>
      <span className="text-2xl font-bold text-[#e4e1e9]">{value}</span>
    </div>
  )
}

function FeatureCard({ icon, title, desc }: { icon: string; title: string; desc: string }) {
  const iconPath = icon === 'mic'
    ? 'M12 14c1.66 0 3-1.34 3-3V5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3z'
    : icon === 'sparkle'
      ? 'M12 2L9.19 8.63 2 9.24l5.46 4.73L5.82 21 12 17.27 18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2z'
      : 'M7 18h2V6H7v12zm4-12v12h2V6h-2zm-8 8h2v-4H3v4zm12-6v8h2V8h-2zm4 2v4h2v-4h-2z'
  return (
    <div className="bg-[#1b1b20] p-8 rounded-xl flex flex-col items-center text-center transition-all hover:bg-[#2a292f] group">
      <div className="w-12 h-12 bg-[#0e0e13] rounded-lg flex items-center justify-center mb-6 text-[#947dff] group-hover:scale-110 transition-transform">
        <svg className="w-6 h-6" viewBox="0 0 24 24" fill="currentColor"><path d={iconPath} /></svg>
      </div>
      <h3 className="text-xl font-bold text-[#e4e1e9] mb-2">{title}</h3>
      <p className="text-sm text-[#b5b3c4]">{desc}</p>
    </div>
  )
}

function ProviderCard({ selected, onClick, title, desc, recommended }: {
  selected: boolean; onClick: () => void; title: string; desc: string; recommended?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'w-full text-left p-6 rounded-xl transition-all',
        selected
          ? 'bg-[#2a292f] border-l-4 border-[#cabeff] ambient-glow'
          : 'bg-[#1b1b20] hover:bg-[#2a292f] border-l-4 border-transparent',
      ].join(' ')}
    >
      <div className="flex items-start justify-between">
        <div>
          <h3 className="text-[#e4e1e9] font-semibold text-lg">{title}</h3>
          <p className="text-[#b5b3c4] text-sm mt-1">{desc}</p>
        </div>
        <div className="flex items-center gap-2">
          {recommended && (
            <span className="text-[10px] font-bold tracking-widest uppercase px-2 py-1 bg-[#cabeff]/20 text-[#cabeff] rounded-full">
              Recommended
            </span>
          )}
          {selected && (
            <svg className="w-5 h-5 text-[#cabeff]" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
            </svg>
          )}
        </div>
      </div>
    </button>
  )
}

function WizardFeatureCard({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="bg-[#1b1b20] p-6 rounded-xl hover:bg-[#2a292f] transition-all flex flex-col items-center text-center">
      <h3 className="text-[#e4e1e9] font-bold mb-1">{title}</h3>
      <p className="text-[#b5b3c4] text-sm">{desc}</p>
    </div>
  )
}

function QuickStartCard({ number, title, children }: { number: string; title: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-3 rounded-xl bg-[#0e0e13] px-4 py-3">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-[#1f1f25] text-[#cabeff]">
        <span className="text-xs font-bold">{number}</span>
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-[#e4e1e9]">{title}</p>
        <p className="mt-1 text-xs leading-6 text-[#938ea1]">{children}</p>
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
    <div data-testid="quicknote-row" className="group rounded-lg border border-[#484555]/30 bg-[#0e0e13] px-3 py-2">
      <p className="line-clamp-3 text-xs leading-relaxed text-[#e4e1e9]/70">{note.text}</p>
      <div className="mt-1.5 flex items-center gap-2">
        <span className="text-[10px] text-[#938ea1]">{formatLibraryTimestamp(note.createdAt)}</span>
        {note.pinned && (
          <span className="rounded bg-[#cabeff]/12 px-1.5 py-0.5 text-[10px] text-[#cabeff]">Pinned</span>
        )}
        {note.provider && note.provider !== 'manual' && (
          <span className="rounded bg-[#35343a] px-1.5 py-0.5 text-[10px] text-[#938ea1]">{note.provider}</span>
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
                href={dashboardAudioDownloadURL('quicknote', note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]"
                aria-label="Download audio"
              >
                Download audio
              </a>
              <button
                type="button"
                onClick={() => void onRevealAudio('quicknote', note.id)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]"
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
              note.pinned ? 'text-[#cabeff] hover:bg-[#cabeff]/10' : 'text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]'
            }`}
          >
            {note.pinned ? 'Unpin' : 'Pin'}
          </button>
          <button
            type="button"
            onClick={() => fetch(`/quicknotes/open-editor?id=${note.id}`, { method: 'POST' })}
            className="rounded px-1.5 py-0.5 text-[10px] text-[#cabeff]/60 hover:bg-[#cabeff]/10 hover:text-[#cabeff]"
          >
            Edit
          </button>
          <button
            type="button"
            onClick={() => onCopy(note.id, note.text)}
            className="rounded px-1.5 py-0.5 text-[10px] text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]"
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
    <div data-testid="transcription-row" className="group flex items-start gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-[#35343a]/40">
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm leading-snug text-[#e4e1e9]/80">{record.text}</p>
        <div className="mt-1 flex items-center gap-1.5 overflow-hidden">
          <span className="shrink-0 rounded bg-[#35343a] px-1.5 py-0.5 text-[10px] font-medium text-[#938ea1]">
            {record.provider}
          </span>
          {record.model && (
            <span className="shrink-0 truncate rounded bg-[#cabeff]/12 px-1.5 py-0.5 text-[10px] text-[#cabeff]">
              {formatTranscriptionModelLabel(record.model)}
            </span>
          )}
          {record.audio && (
            <span className="shrink-0 rounded bg-emerald-500/12 px-1.5 py-0.5 text-[10px] text-emerald-200/90">
              {formatAudioDuration(record.audio.durationMs)}
            </span>
          )}
          <span className="shrink-0 text-[11px] text-[#938ea1]">{formatLibraryTimestamp(record.createdAt)}</span>
        </div>
      </div>
      <div className="mt-0.5 flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
        {record.audio && (
          <>
            <a
              href={dashboardAudioDownloadURL('transcription', record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]"
              aria-label="Download audio"
            >
              Download audio
            </a>
            <button
              type="button"
              onClick={() => void onRevealAudio('transcription', record.id)}
              className="rounded px-1.5 py-0.5 text-[10px] text-[#938ea1] hover:bg-[#35343a] hover:text-[#e4e1e9]"
              aria-label="Show file"
            >
              Show file
            </button>
          </>
        )}
        <button
          type="button"
          onClick={() => onCopy(record.id, record.text)}
          className="flex h-7 w-7 items-center justify-center rounded-md text-[#938ea1] transition-colors hover:bg-[#35343a] hover:text-[#cabeff]"
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

function ClipboardIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
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
  if (typeof window === 'undefined') return 'dashboard'
  const hashTab = parseDashboardTab(window.location.hash)
  if (hashTab) return hashTab
  const storedTab = parseDashboardTab(window.sessionStorage.getItem(DASHBOARD_TAB_STORAGE_KEY) ?? '')
  if (storedTab) return storedTab
  return 'dashboard'
}

function parseDashboardTab(value: string): Tab | null {
  const normalized = value.replace(/^#/, '').trim().toLowerCase()
  switch (normalized) {
    case 'dashboard':
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
    const date = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: '2-digit', year: 'numeric' }).format(d)
    const time = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false }).format(d)
    return `${date} · ${time}`
  } catch { return '' }
}

function formatStatNumber(value?: number): string {
  if (typeof value !== 'number') return '--'
  return new Intl.NumberFormat('en-GB').format(value)
}

function formatAverageWPM(value?: number): string {
  if (typeof value !== 'number' || Number.isNaN(value) || value <= 0) return '--'
  return value.toFixed(1)
}

function formatRecordedMinutes(durationMs?: number): string {
  if (typeof durationMs !== 'number' || durationMs <= 0) return '--'
  return (durationMs / 60000).toFixed(1)
}

function formatAudioDuration(durationMs: number) {
  const seconds = durationMs / 1000
  if (seconds >= 60) return `${(seconds / 60).toFixed(1)}m`
  return `${seconds.toFixed(1)}s`
}

function formatTranscriptionModelLabel(model: string) {
  const normalized = model.trim()
  if (!normalized) return ''
  if (normalized.endsWith('whisper-large-v3-turbo')) return 'turbo-v3'
  if (normalized.endsWith('whisper-large-v3')) return 'large-v3'
  const leaf = normalized.split(/[\\/]/).pop() ?? normalized
  return leaf.replace(/\.(bin|gguf|onnx)$/i, '')
}

function formatLogTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return '' }
}

function logColor(type: string): string {
  switch (type) {
    case 'error': return 'text-red-400'
    case 'warn': return 'text-yellow-400'
    case 'success': return 'text-green-400'
    default: return 'text-[#938ea1]'
  }
}

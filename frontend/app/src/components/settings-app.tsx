import { useEffect, useRef, useState } from 'react'

import { MicSelector } from '@/components/ui/mic-selector'
import {
  clearHuggingFaceToken,
  defaultSettingsState,
  fetchModelProfiles,
  fetchSettingsState,
  saveHuggingFaceToken,
  saveSettingsState,
  type ModelProfile,
  type SpeechKitSettingsState,
} from '@/lib/speechkit'

type Tab = 'general' | 'provider'

const CTRL_SHIFT_SUFFIX_KEYS = ['d', 'j', 'k', 'space'] as const

export function SettingsApp() {
  const [settings, setSettings] = useState(defaultSettingsState)
  const [hfToken, setHFToken] = useState('')
  const [hfTokenBusy, setHFTokenBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [toast, setToast] = useState('')
  const [tab, setTab] = useState<Tab>('general')
  const saveTimer = useRef<number | null>(null)
  const toastTimer = useRef<number | null>(null)

  const loadSettings = async () => {
    const [state, profiles] = await Promise.all([
      fetchSettingsState(),
      fetchModelProfiles().catch(() => []),
    ])

    setSettings({
      ...state,
      profiles: state.profiles?.length ? state.profiles : profiles,
    })
  }

  useEffect(() => {
    let active = true

    void loadSettings().then(() => {
      if (!active) return
      setLoaded(true)
    }).catch(() => {
      if (!active) return
      setLoaded(true)
    })

    return () => {
      active = false
      if (saveTimer.current) window.clearTimeout(saveTimer.current)
      if (toastTimer.current) window.clearTimeout(toastTimer.current)
    }
  }, [])

  const showToast = (message: string) => {
    if (toastTimer.current) window.clearTimeout(toastTimer.current)
    setToast(message)
    toastTimer.current = window.setTimeout(() => setToast(''), 1400)
  }

  const queueSave = (next: SpeechKitSettingsState, delay: number) => {
    setSettings(next)
    if (!loaded) return
    if (saveTimer.current) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(async () => {
      try {
        const message = await saveSettingsState(next)
        showToast(message || 'Saved')
      } catch {
        showToast('Save failed')
      }
    }, delay)
  }

  const updateSettings = (patch: Partial<SpeechKitSettingsState>, delay = 0) => {
    queueSave({ ...settings, ...patch }, delay)
  }

  const tokenStatusLabel = () => {
    switch (settings.hfTokenSource) {
      case 'user':
        return 'User token active'
      case 'install':
        return 'Install token active'
      case 'env':
        return 'Environment token active'
      default:
        return 'No token configured'
    }
  }

  const handleSaveHuggingFaceToken = async () => {
    const trimmed = hfToken.trim()
    if (!trimmed) {
      showToast('Token required')
      return
    }
    setHFTokenBusy(true)
    try {
      const message = await saveHuggingFaceToken(trimmed)
      await loadSettings()
      setHFToken('')
      showToast(message || 'Saved')
    } catch {
      showToast('Save failed')
    } finally {
      setHFTokenBusy(false)
    }
  }

  const handleClearHuggingFaceToken = async () => {
    setHFTokenBusy(true)
    try {
      const message = await clearHuggingFaceToken()
      await loadSettings()
      setHFToken('')
      showToast(message || 'Saved')
    } catch {
      showToast('Save failed')
    } finally {
      setHFTokenBusy(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#0b0f14] px-5 py-5 text-[13px] text-white/90">
      <div className="mx-auto max-w-[560px]">
        <h1 className="text-base font-semibold tracking-tight">
          SpeechKit Settings
        </h1>

        <div className="mt-3 flex gap-px rounded-lg bg-white/5 p-0.5">
          <TabBtn active={tab === 'general'} onClick={() => setTab('general')}>
            General
          </TabBtn>
          <TabBtn
            active={tab === 'provider'}
            onClick={() => setTab('provider')}
          >
            Provider
          </TabBtn>
        </div>

        {tab === 'general' && (
          <div className="mt-4 flex flex-col gap-5">
            <Section title="Mode">
              <div className="flex flex-wrap gap-1.5">
                <Chip
                  active={settings.activeMode === 'dictate'}
                  onClick={() => updateSettings({ activeMode: 'dictate' })}
                >
                  Dictate
                </Chip>
                <Chip
                  active={settings.activeMode === 'agent'}
                  onClick={() => updateSettings({ activeMode: 'agent' })}
                >
                  Agent
                </Chip>
              </div>
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                The active mode decides which action wins when hotkeys overlap.
              </p>
            </Section>

            <Section title="Hotkeys">
              <HotkeyPicker
                label="Dictate hotkey"
                value={settings.dictateHotkey}
                onChange={(value) =>
                  updateSettings({ dictateHotkey: value, hotkey: value })
                }
              />
              <div className="mt-3" />
              <HotkeyPicker
                label="Agent hotkey"
                value={settings.agentHotkey}
                onChange={(value) => updateSettings({ agentHotkey: value })}
              />
            </Section>

            <Section title="Microphone">
              <MicSelector
                value={settings.selectedAudioDeviceId}
                onValueChange={(deviceId) =>
                  updateSettings({ selectedAudioDeviceId: deviceId })
                }
                className="w-full"
              />
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                The desktop backend applies the selected device immediately; no restart flow is needed.
              </p>
            </Section>

            <Section title="Overlay">
              <Row
                label="Show overlay"
                on={settings.overlayEnabled}
                onToggle={() =>
                  updateSettings({ overlayEnabled: !settings.overlayEnabled })
                }
              />
              {settings.overlayEnabled && (
                <>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <Chip
                      active={settings.visualizer === 'pill'}
                      onClick={() => updateSettings({ visualizer: 'pill' })}
                    >
                      Default <span className="ml-1 text-[10px] opacity-50">(Pill)</span>
                    </Chip>
                    <Chip
                      active={settings.visualizer === 'circle'}
                      onClick={() => updateSettings({ visualizer: 'circle' })}
                    >
                      Focus <span className="ml-1 text-[10px] opacity-50">(Dot)</span>
                    </Chip>
                    {settings.visualizer === 'pill' && (
                      <>
                        <span className="mx-1 self-center text-white/20">|</span>
                        <Chip
                          active={settings.design === 'default'}
                          onClick={() => updateSettings({ design: 'default' })}
                        >
                          Default
                        </Chip>
                        <Chip
                          active={settings.design === 'kombify'}
                          onClick={() => updateSettings({ design: 'kombify' })}
                        >
                          kombify
                        </Chip>
                      </>
                    )}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <span className="mr-1 self-center text-[11px] text-white/35">
                      Position
                    </span>
                    {(['top', 'bottom', 'left', 'right'] as const).map((pos) => (
                      <Chip
                        key={pos}
                        active={settings.overlayPosition === pos}
                        onClick={() => updateSettings({ overlayPosition: pos })}
                      >
                        {pos.charAt(0).toUpperCase() + pos.slice(1)}
                      </Chip>
                    ))}
                  </div>
                </>
              )}
            </Section>

            <Section title="Storage">
              <Row
                label="Save raw audio locally"
                on={settings.saveAudio}
                onToggle={() => updateSettings({ saveAudio: !settings.saveAudio })}
              />
              <label
                htmlFor="audio-retention-select"
                className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Audio retention
              </label>
              <select
                id="audio-retention-select"
                aria-label="Audio retention"
                value={String(settings.audioRetentionDays)}
                onChange={(e) =>
                  updateSettings({ audioRetentionDays: Number(e.target.value) })
                }
                className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
              >
                <option value="0">No automatic deletion</option>
                <option value="1">1 day</option>
                <option value="7">7 days</option>
                <option value="30">30 days</option>
                <option value="90">90 days</option>
              </select>
              <p className="mt-1.5 text-xs leading-relaxed text-white/40">
                SQLite keeps metadata locally. Audio stays as raw WAV files on disk and can be turned off or pruned automatically.
              </p>
            </Section>
          </div>
        )}

        {tab === 'provider' && (
          <div className="mt-4 flex flex-col gap-5">
            <Section title="Speech-to-Text Provider">
              <Row
                label="Hugging Face Inference"
                on={settings.hfEnabled}
                onToggle={() => updateSettings({ hfEnabled: !settings.hfEnabled })}
              />
              <div className="mt-2 rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-white/65">
                {tokenStatusLabel()}
              </div>
              <label
                htmlFor="hf-token-input"
                className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Hugging Face token
              </label>
              <input
                id="hf-token-input"
                aria-label="Hugging Face token"
                type="password"
                value={hfToken}
                onChange={(e) => setHFToken(e.target.value)}
                placeholder="hf_..."
                className="mt-2 h-9 w-full rounded-lg border border-white/10 bg-white/5 px-3 text-xs outline-none focus:border-orange-400/50"
              />
              <div className="mt-2 flex gap-2">
                <Chip
                  active={false}
                  onClick={() => {
                    void handleSaveHuggingFaceToken()
                  }}
                  className={hfTokenBusy ? 'pointer-events-none opacity-60' : ''}
                >
                  Save token
                </Chip>
                {settings.hfHasUserToken && (
                  <Chip
                    active={false}
                    onClick={() => {
                      void handleClearHuggingFaceToken()
                    }}
                    className={hfTokenBusy ? 'pointer-events-none opacity-60' : ''}
                  >
                    Clear token
                  </Chip>
                )}
              </div>
              <label
                htmlFor="hf-model-select"
                className="mt-2 block text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35"
              >
                Model
              </label>
              <select
                id="hf-model-select"
                aria-label="Model"
                value={settings.hfModel}
                onChange={(e) =>
                  updateSettings({
                    hfModel: e.target.value as SpeechKitSettingsState['hfModel'],
                  })
                }
                className={[
                  'mt-2 h-9 w-full rounded-lg border px-3 text-xs outline-none',
                  settings.hfEnabled
                    ? 'border-white/10 bg-white/5 focus:border-orange-400/50'
                    : 'border-white/6 bg-white/[0.02] text-white/40',
                ].join(' ')}
              >
                <option value="openai/whisper-large-v3-turbo">
                  whisper-large-v3-turbo
                </option>
                <option value="openai/whisper-large-v3">
                  whisper-large-v3
                </option>
              </select>
              {!settings.hfEnabled && (
                <p className="mt-1.5 rounded-lg border border-orange-500/20 bg-orange-500/5 px-3 py-2 text-xs leading-relaxed text-orange-200/70">
                  Cloud inference is currently disabled in this config. You can
                  still choose a model here. Local and VPS providers can be
                  configured in config.toml, and internal builds may enable a
                  managed provider automatically.
                </p>
              )}
            </Section>

            {settings.profiles && settings.profiles.length > 0 && (
              <Section title="Model Profiles" badge={`${settings.profiles.length}`}>
                <div className="flex flex-col gap-2">
                  {settings.profiles.map((profile) => (
                    <ProfileCard
                      key={profile.id}
                      profile={profile}
                      active={settings.activeProfiles?.[profile.modality] === profile.id}
                    />
                  ))}
                </div>
              </Section>
            )}

            <Section title="Privacy">
              <p className="text-xs leading-relaxed text-white/40">
                Audio is sent to external servers for transcription only when you
                explicitly enable an inference-backed provider. Local mode and
                self-hosted providers keep the runtime on your machine.
              </p>
            </Section>
          </div>
        )}
      </div>

      <div
        className={[
          'pointer-events-none fixed top-4 right-4 rounded-lg border border-emerald-400/20 bg-emerald-500/10 px-3 py-1.5 text-xs text-emerald-200 transition-all',
          toast ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0',
        ].join(' ')}
      >
        {toast || '\u00A0'}
      </div>
    </div>
  )
}

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

function Section({
  title,
  badge,
  children,
}: {
  title: string
  badge?: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-white/35">
          {title}
        </span>
        {badge && (
          <span className="rounded bg-white/6 px-1.5 py-0.5 text-[10px] text-white/25">
            {badge}
          </span>
        )}
      </div>
      {children}
    </section>
  )
}

function HotkeyPicker({
  label,
  value,
  onChange,
}: {
  label: string
  value: string
  onChange: (value: string) => void
}) {
  const isCtrlShift = value.startsWith('ctrl+shift+')
  const ctrlShiftSuffix = isCtrlShift ? value.replace('ctrl+shift+', '') : 'd'

  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-white/65">{label}</div>
      <div className="flex flex-wrap gap-1.5">
        <Chip active={value === 'win+alt'} onClick={() => onChange('win+alt')}>
          Win + Alt
        </Chip>
        <Chip
          active={value === 'ctrl+win'}
          onClick={() => onChange('ctrl+win')}
        >
          Ctrl + Win
        </Chip>
        <span className="flex items-center gap-0">
          <Chip
            active={isCtrlShift}
            onClick={() => onChange(`ctrl+shift+${ctrlShiftSuffix}`)}
            className={isCtrlShift ? 'rounded-r-none' : ''}
          >
            Ctrl + Shift +
          </Chip>
          <select
            value={ctrlShiftSuffix}
            onChange={(e) => onChange(`ctrl+shift+${e.target.value}`)}
            className={[
              'h-8 rounded-r-lg border-y border-r px-2 text-xs font-medium uppercase outline-none',
              isCtrlShift
                ? 'border-orange-500/60 bg-orange-500/20 text-orange-200'
                : 'border-white/12 bg-white/5 text-white/50',
            ].join(' ')}
          >
            {CTRL_SHIFT_SUFFIX_KEYS.map((k) => (
              <option key={k} value={k}>
                {k === 'space' ? 'Space' : k.toUpperCase()}
              </option>
            ))}
          </select>
        </span>
      </div>
    </div>
  )
}

function ProfileCard({
  profile,
  active,
}: {
  profile: ModelProfile
  active: boolean
}) {
  return (
    <div
      className={[
        'rounded-lg border px-3 py-2 text-xs transition-colors',
        active
          ? 'border-orange-400/30 bg-orange-500/8 text-orange-100'
          : 'border-white/8 bg-white/[0.03] text-white/65',
      ].join(' ')}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{profile.name}</div>
          <div className="truncate text-[11px] text-white/35">
            {profile.modality.toUpperCase()} · {profile.executionMode ?? 'local'}
          </div>
        </div>
        {active && (
          <span className="rounded bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-medium text-orange-200">
            Active
          </span>
        )}
      </div>
      {profile.description ? (
        <p className="mt-1.5 text-[11px] leading-relaxed text-white/40">
          {profile.description}
        </p>
      ) : null}
    </div>
  )
}

function Chip({
  active,
  onClick,
  children,
  className = '',
}: {
  active: boolean
  onClick?: () => void
  children: React.ReactNode
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'h-8 rounded-lg border px-3 text-xs font-medium transition-all',
        active
          ? 'border-orange-500/60 bg-orange-500/20 text-orange-200'
          : 'border-white/12 bg-white/5 text-white/50 hover:border-white/20 hover:text-white/70',
        className,
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function Row({
  label,
  on,
  onToggle,
}: {
  label: string
  on: boolean
  onToggle: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-sm text-white/70">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={on}
        onClick={onToggle}
        className={[
          'relative inline-flex h-[22px] w-[38px] flex-shrink-0 cursor-pointer items-center rounded-full transition-colors',
          on ? 'bg-orange-500' : 'bg-white/15',
        ].join(' ')}
      >
        <span
          className={[
            'inline-block h-[16px] w-[16px] rounded-full bg-white shadow transition-transform',
            on ? 'translate-x-[19px]' : 'translate-x-[3px]',
          ].join(' ')}
        />
      </button>
    </div>
  )
}

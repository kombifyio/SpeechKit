import { useEffect, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import {
  AudioLines,
  Bot,
  ClipboardCopy,
  FileText,
  Headphones,
  Mic,
  type LucideIcon,
} from 'lucide-react'

import { AgentAudioVisualizerBar } from '@/components/agent-audio-visualizer-bar'
import { AgentAudioVisualizerRadial } from '@/components/agent-audio-visualizer-radial'
import { DotRadialMenu, type DotMenuItem } from '@/components/dot-radial-menu'
import {
  resolveOverlayTone,
  type OverlayTone,
} from '@/components/overlay-tone'
import { useAudioDevices } from '@/components/ui/use-audio-devices'
import { useOverlaySnapshot } from '@/hooks/use-overlay-snapshot'
import {
  setModeEnabled,
  type RuntimeMode,
  type SpeechKitOverlayState,
} from '@/lib/speechkit'

type ConfigurableMode = Exclude<RuntimeMode, 'none'>

const MODE_ORDER: ConfigurableMode[] = ['dictate', 'assist', 'voice_agent']
const MODE_HOTKEY_FIELDS = {
  dictate: 'dictateHotkey',
  assist: 'assistHotkey',
  voice_agent: 'voiceAgentHotkey',
} as const satisfies Record<ConfigurableMode, keyof SpeechKitOverlayState>
const MODE_META: Record<
  ConfigurableMode,
  { label: string; statusLabel: string; icon: LucideIcon; iconKey: string }
> = {
  dictate: {
    label: 'Dictation',
    statusLabel: 'Dictate',
    icon: AudioLines,
    iconKey: 'audio-lines',
  },
  assist: {
    label: 'Assist',
    statusLabel: 'Assist',
    icon: Bot,
    iconKey: 'bot',
  },
  voice_agent: {
    label: 'Voice Agent',
    statusLabel: 'Voice Agent',
    icon: Headphones,
    iconKey: 'headphones',
  },
}

function OverlaySurfaceFrame({
  children,
}: {
  children: ReactNode
}) {
  return <div className="pointer-events-none relative h-full w-full overflow-visible">{children}</div>
}

function OverlayActionButton({
  icon,
  title,
  onClick,
  disabled,
  active,
  className,
}: {
  icon: ReactNode
  title: string
  onClick?: () => void
  disabled?: boolean
  active?: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={title}
      style={{ WebkitAppRegion: 'no-drag' } as CSSProperties}
      className={[
        'flex h-7 w-7 items-center justify-center rounded-full transition-colors',
        active
          ? 'bg-orange-500/20 text-orange-100 ring-1 ring-orange-400/30'
          : '',
        disabled
          ? 'cursor-not-allowed text-white/25'
          : 'text-white/75 hover:bg-white/15 active:bg-white/25',
        className ?? '',
      ].join(' ')}
    >
      {icon}
    </button>
  )
}

function OverlayQuickNoteDot() {
  return <div className="h-2 w-2 animate-pulse rounded-full bg-orange-400" />
}

function OverlayModeAction({
  label,
  icon,
  enabled,
  runtimeActive,
  slashed,
  onClick,
}: {
  label: string
  icon: ReactNode
  enabled: boolean
  runtimeActive: boolean
  slashed: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      aria-pressed={enabled}
      data-runtime-active={runtimeActive ? 'true' : 'false'}
      style={{ WebkitAppRegion: 'no-drag' } as CSSProperties}
      className="relative"
      data-testid={`mode-toggle-${label.toLowerCase().replace(/\s+/g, '-')}`}
    >
      <span
        className={[
          'flex h-6 w-6 items-center justify-center rounded-full border transition-colors',
          enabled
            ? 'border-white/18 bg-white/8 text-white/85 hover:border-white/28 hover:text-white'
            : 'border-white/8 bg-white/[0.03] text-white/35 hover:border-white/12 hover:text-white/55',
          runtimeActive ? 'ring-1 ring-orange-400/40 text-orange-100 border-orange-400/30 bg-orange-500/18' : '',
        ].join(' ')}
      >
        {icon}
      </span>
      {slashed ? (
        <span
          data-testid={`mode-toggle-${label.toLowerCase().replace(/\s+/g, '-')}-slashed`}
          aria-hidden="true"
          className="pointer-events-none absolute left-1/2 top-1/2 h-0.5 w-5 -translate-x-1/2 -translate-y-1/2 rotate-[-35deg] rounded-full bg-white/45"
        />
      ) : null}
    </button>
  )
}

function OverlayPanelSection({
  testId,
  className,
  children,
}: {
  testId: string
  className?: string
  children: ReactNode
}) {
  return (
    <div
      data-testid={testId}
      className={['flex items-center gap-0.5', className ?? ''].join(' ')}
    >
      {children}
    </div>
  )
}

function OverlayMicrophoneQuickSelect({
  selectedDeviceId,
}: {
  selectedDeviceId: string
}) {
  const { devices, selectedDeviceId: detectedDeviceId, loading, setSelectedDevice } = useAudioDevices()
  const [localSelectedDeviceId, setLocalSelectedDeviceId] = useState(selectedDeviceId)

  useEffect(() => {
    setLocalSelectedDeviceId(selectedDeviceId)
  }, [selectedDeviceId])

  const resolvedSelectedDeviceId =
    localSelectedDeviceId || detectedDeviceId || devices[0]?.deviceId || ''
  const currentDevice =
    devices.find((device) => device.deviceId === resolvedSelectedDeviceId) ??
    devices[0]

  const handleDeviceChange = (nextDeviceId: string) => {
    if (!nextDeviceId) {
      return
    }
    setLocalSelectedDeviceId(nextDeviceId)
    void setSelectedDevice(nextDeviceId).catch(() => {
      setLocalSelectedDeviceId(selectedDeviceId || detectedDeviceId || '')
    })
  }

  return (
    <div
      data-testid="pill-panel-mic-selector"
      title={currentDevice?.label ?? 'Microphone quick select'}
      className={[
        'relative flex h-6 w-6 items-center justify-center overflow-hidden rounded-full border transition-colors',
        loading || devices.length === 0
          ? 'border-white/8 bg-white/4 text-white/25'
          : 'border-white/10 bg-white/6 text-white/75 hover:border-white/18 hover:bg-white/15 hover:text-white/95',
      ].join(' ')}
      style={{ WebkitAppRegion: 'no-drag' } as CSSProperties}
    >
      <Mic className="h-3.5 w-3.5" data-testid="mic-selector-icon" data-icon="mic" />
      <select
        data-testid="pill-panel-mic-select"
        aria-label="Microphone quick select"
        value={resolvedSelectedDeviceId}
        disabled={loading || devices.length === 0}
        onChange={(event) => {
          handleDeviceChange(event.target.value)
        }}
        className="absolute inset-0 cursor-pointer appearance-none opacity-0 disabled:cursor-not-allowed"
      >
        {devices.map((device) => (
          <option key={device.deviceId} value={device.deviceId}>
            {device.label}
          </option>
        ))}
      </select>
    </div>
  )
}

function BubbleGlyph() {
  return (
    <svg
      data-testid="dot-anchor-glyph"
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="pointer-events-none absolute left-1/2 top-1/2 z-0 h-2.75 w-2.75 -translate-x-1/2 -translate-y-1/2 opacity-45"
    >
      <path
        d="M12 2.5C7.03 2.5 3 6.08 3 10.5c0 2.18 1 4.16 2.63 5.6-.17 1.47-.78 3.08-1.88 4.4 2.35-.16 4.2-.93 5.54-2.02.83.21 1.7.32 2.71.32 4.97 0 9-3.58 9-8S16.97 2.5 12 2.5Z"
        fill="currentColor"
      />
    </svg>
  )
}

function modeAvailable(snapshot: SpeechKitOverlayState, mode: ConfigurableMode) {
  return snapshot.availableModes?.[mode] ?? (snapshot[MODE_HOTKEY_FIELDS[mode]].trim().length > 0)
}

function modeEnabled(snapshot: SpeechKitOverlayState, mode: ConfigurableMode) {
  return snapshot.modeEnabled?.[mode] ?? modeAvailable(snapshot, mode)
}

function modeStatusLabel(mode: RuntimeMode) {
  if (mode === 'none') {
    return 'No mode'
  }
  return MODE_META[mode].statusLabel
}

function activeModeTitle(mode: ConfigurableMode) {
  return `Active mode: ${MODE_META[mode].label}`
}

function ModeGlyph({
  mode,
  className,
  title,
}: {
  mode: ConfigurableMode
  className?: string
  title?: string
}) {
  const Icon = MODE_META[mode].icon

  return (
    <div
      data-testid={`mode-glyph-${mode}`}
      title={title}
      className={[
        'pointer-events-none flex h-5 w-5 items-center justify-center rounded-full bg-white/8 text-white/80',
        className ?? '',
      ].join(' ')}
    >
      <Icon className="h-3 w-3" />
    </div>
  )
}

function shouldShowActiveModeBadge(snapshot: SpeechKitOverlayState) {
  return snapshot.activeMode !== 'none' && snapshot.state !== 'idle'
}

function toggleMode(mode: ConfigurableMode, snapshot: SpeechKitOverlayState) {
  void setModeEnabled(mode, !modeEnabled(snapshot, mode))
}

function postOverlayFreeCenter(url: string, centerX: number, centerY: number) {
  return fetch(url, {
    method: 'POST',
    body: new URLSearchParams({
      center_x: String(Math.round(centerX)),
      center_y: String(Math.round(centerY)),
    }),
  })
}

function OverlayPillShell({
  snapshot,
  tone,
  shellClassName,
  surface,
  draggable = false,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  onPointerCancel,
  children,
}: {
  snapshot: SpeechKitOverlayState
  tone: OverlayTone
  shellClassName: string
  surface: string
  draggable?: boolean
  onPointerDown?: (event: ReactPointerEvent<HTMLDivElement>) => void
  onPointerMove?: (event: ReactPointerEvent<HTMLDivElement>) => void
  onPointerUp?: (event: ReactPointerEvent<HTMLDivElement>) => void
  onPointerCancel?: (event: ReactPointerEvent<HTMLDivElement>) => void
  children: ReactNode
}) {
  const showKombifyMark = snapshot.visualizer === 'pill' && snapshot.design === 'kombify'

  return (
    <div
      data-testid={`${surface}-shell`}
      data-overlay-surface={surface}
      data-overlay-mode="pill"
      data-overlay-state={snapshot.state}
      data-overlay-phase={snapshot.phase}
      data-active-mode={snapshot.activeMode}
      data-overlay-size={tone.size}
      data-overlay-color={tone.color}
      data-overlay-draggable={draggable ? 'true' : 'false'}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      className={[
        'relative flex select-none items-center justify-center transition-all duration-200 ease-out',
        shellClassName,
        tone.className,
      ].join(' ')}
    >
      <span data-testid={`${surface}-status`} className="sr-only">
        {overlayStatusLabel(snapshot)}
      </span>
      <div className="relative flex items-center justify-center">
        {children}
        {showKombifyMark ? (
          <img
            alt="kombify idle mark"
            data-testid={`${surface}-kombify-mark`}
            src="/idle-kombify.png"
            className="pointer-events-none absolute left-1/2 top-1/2 z-0 h-2 w-auto -translate-x-1/2 -translate-y-1/2 opacity-55"
          />
        ) : null}
      </div>
      {shouldShowActiveModeBadge(snapshot) ? (
        <div className="absolute right-1.5 top-1/2 -translate-y-1/2">
          <ModeGlyph
            mode={snapshot.activeMode as ConfigurableMode}
            title={activeModeTitle(snapshot.activeMode as ConfigurableMode)}
          />
        </div>
      ) : null}
      {snapshot.quickNoteMode ? <div className="absolute -top-1 -right-1"><OverlayQuickNoteDot /></div> : null}
    </div>
  )
}

function PillAnchorOverlayView({
  snapshot,
}: {
  snapshot: SpeechKitOverlayState
}) {
  const tone = resolveOverlayTone(snapshot)

  return (
    <OverlaySurfaceFrame>
      <div
        data-testid="pill-anchor-stage"
        className="pointer-events-auto absolute inset-0 flex items-center justify-center"
        onMouseEnter={() => {
          void fetch('/overlay/pill-panel/show', { method: 'POST' })
        }}
      >
        <OverlayPillShell
          snapshot={snapshot}
          tone={tone}
          shellClassName={tone.shellClassName}
          surface="pill-anchor"
        >
          <AgentAudioVisualizerBar
            data-testid="pill-anchor-visualizer"
            size={tone.size}
            state={tone.state}
            level={tone.level}
            barCount={5}
            color={tone.color}
            className={['relative z-10 text-current', tone.visualizerClassName].join(' ')}
          />
        </OverlayPillShell>
      </div>
    </OverlaySurfaceFrame>
  )
}

function PillPanelOverlayView({
  snapshot,
}: {
  snapshot: SpeechKitOverlayState
}) {
  const tone = resolveOverlayTone(snapshot)
  const dragStateRef = useRef<{
    pointerId: number
    startScreenX: number
    startScreenY: number
    startCenterX: number
    startCenterY: number
    lastCenterX: number
    lastCenterY: number
    moved: boolean
  } | null>(null)
  const copyLast = () => {
    if (snapshot.lastTranscription) {
      void navigator.clipboard.writeText(snapshot.lastTranscription)
    }
  }

  const openQuickCapture = () => {
    void fetch('/quicknotes/open-capture', { method: 'POST' })
  }

  const beginPanelDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!snapshot.movable || event.button !== 0) {
      return
    }

    dragStateRef.current = {
      pointerId: event.pointerId,
      startScreenX: event.screenX,
      startScreenY: event.screenY,
      startCenterX: snapshot.positionFreeX,
      startCenterY: snapshot.positionFreeY,
      lastCenterX: snapshot.positionFreeX,
      lastCenterY: snapshot.positionFreeY,
      moved: false,
    }

    if (typeof event.currentTarget.setPointerCapture === 'function') {
      try {
        event.currentTarget.setPointerCapture(event.pointerId)
      } catch {
        // JSDOM and some WebView edge cases do not fully support pointer capture.
      }
    }
    event.preventDefault()
  }

  const movePanelDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const session = dragStateRef.current
    if (!session || session.pointerId !== event.pointerId) {
      return
    }

    const nextCenterX = session.startCenterX + Math.round(event.screenX - session.startScreenX)
    const nextCenterY = session.startCenterY + Math.round(event.screenY - session.startScreenY)
    if (nextCenterX === session.lastCenterX && nextCenterY === session.lastCenterY) {
      return
    }

    session.lastCenterX = nextCenterX
    session.lastCenterY = nextCenterY
    session.moved = true

    void postOverlayFreeCenter('/overlay/free-center', nextCenterX, nextCenterY)
    event.preventDefault()
  }

  const endPanelDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    const session = dragStateRef.current
    if (!session || session.pointerId !== event.pointerId) {
      return
    }
    dragStateRef.current = null

    if (typeof event.currentTarget.releasePointerCapture === 'function') {
      try {
        event.currentTarget.releasePointerCapture(event.pointerId)
      } catch {
        // Pointer capture may already be gone; no action required.
      }
    }

    if (session.moved) {
      void postOverlayFreeCenter('/overlay/free-center/save', session.lastCenterX, session.lastCenterY)
    }
    event.preventDefault()
  }

  return (
    <OverlaySurfaceFrame>
      <div
        data-testid="pill-panel-stage"
        className="pointer-events-auto absolute inset-0 flex items-center justify-center"
        onMouseLeave={() => {
          if (dragStateRef.current) {
            return
          }
          void fetch('/overlay/pill-panel/hide', { method: 'POST' })
        }}
      >
        <div
          data-testid="pill-panel-shell"
          data-overlay-surface="pill-panel"
          data-overlay-mode="pill"
          data-overlay-state={snapshot.state}
          data-overlay-phase={snapshot.phase}
          data-active-mode={snapshot.activeMode}
          data-overlay-size={tone.size}
          data-overlay-color={tone.color}
          aria-label={overlayStatusLabel(snapshot)}
          className="relative grid grid-cols-[76px_auto_76px] items-center gap-1 rounded-full bg-neutral-950/84 px-1.5 py-0.5 shadow-[0_14px_28px_rgba(0,0,0,0.28)] backdrop-blur-xl"
        >
          <span data-testid="pill-panel-status" className="sr-only">
            {overlayStatusLabel(snapshot)}
          </span>
          <OverlayPanelSection testId="pill-panel-left-controls" className="w-[76px] justify-start">
            <OverlayMicrophoneQuickSelect selectedDeviceId={snapshot.selectedAudioDeviceId} />
            <OverlayActionButton
              icon={<ClipboardCopy className="h-3.5 w-3.5" />}
              title="Copy"
              onClick={copyLast}
              className="h-6 w-6"
            />
            <OverlayActionButton
              icon={<FileText className="h-3.5 w-3.5" />}
              title="Note"
              onClick={openQuickCapture}
              className="h-6 w-6"
            />
          </OverlayPanelSection>

          <OverlayPillShell
            snapshot={snapshot}
            tone={tone}
            shellClassName={[
              tone.shellClassName,
              snapshot.movable ? 'cursor-move' : '',
            ].join(' ')}
            surface="pill-panel-center"
            draggable={snapshot.movable}
            onPointerDown={beginPanelDrag}
            onPointerMove={movePanelDrag}
            onPointerUp={endPanelDrag}
            onPointerCancel={endPanelDrag}
          >
            <AgentAudioVisualizerBar
              data-testid="pill-panel-visualizer"
              size={tone.size}
              state={tone.state}
              level={tone.level}
              barCount={5}
              color={tone.color}
              className={['relative z-10 text-current', tone.visualizerClassName].join(' ')}
            />
          </OverlayPillShell>

          <OverlayPanelSection testId="pill-panel-mode-controls" className="w-[76px] justify-end">
            {MODE_ORDER.map((mode) => {
              const Icon = MODE_META[mode].icon
              return (
                <OverlayModeAction
                  key={mode}
                  label={MODE_META[mode].label}
                  icon={(
                    <Icon
                      className="h-3 w-3"
                      data-testid={`mode-icon-${MODE_META[mode].label.toLowerCase()}`}
                      data-icon={MODE_META[mode].iconKey}
                    />
                  )}
                  enabled={modeEnabled(snapshot, mode)}
                  runtimeActive={snapshot.activeMode === mode}
                  slashed={!modeEnabled(snapshot, mode) || !modeAvailable(snapshot, mode)}
                  onClick={() => toggleMode(mode, snapshot)}
                />
              )
            })}
          </OverlayPanelSection>
        </div>
      </div>
    </OverlaySurfaceFrame>
  )
}

function DotAnchorOverlayView({
  snapshot,
}: {
  snapshot: SpeechKitOverlayState
}) {
  const tone = resolveOverlayTone({ ...snapshot, visualizer: 'circle' })

  return (
    <OverlaySurfaceFrame>
      <div
        data-testid="dot-anchor-stage"
        className="pointer-events-auto absolute inset-0 flex items-center justify-center"
      >
        <div
          data-testid="dot-anchor-shell"
          data-overlay-surface="dot-anchor"
          data-overlay-mode="circle"
          data-overlay-state={snapshot.state}
          data-overlay-phase={snapshot.phase}
          data-active-mode={snapshot.activeMode}
          data-overlay-size={tone.size}
          data-overlay-color={tone.color}
          aria-label={overlayStatusLabel(snapshot)}
          className={[
            'relative flex items-center justify-center transition-all duration-150 ease-out',
            tone.className,
            tone.shellClassName,
          ].join(' ')}
          onContextMenu={(event) => {
            event.preventDefault()
            void fetch('/overlay/radial/show', { method: 'POST' })
          }}
        >
          <span data-testid="dot-anchor-status" className="sr-only">
            {overlayStatusLabel(snapshot)}
          </span>
          <AgentAudioVisualizerRadial
            data-testid="dot-anchor-visualizer"
            size={tone.size}
            state={tone.state}
            level={tone.level}
            color={tone.color}
            className={['aspect-square text-current', tone.visualizerClassName].join(' ')}
          />
          <BubbleGlyph />
        </div>
      </div>
    </OverlaySurfaceFrame>
  )
}

function DotRadialOverlayView({
  snapshot,
}: {
  snapshot: SpeechKitOverlayState
}) {
  const tone = resolveOverlayTone({ ...snapshot, visualizer: 'circle' })
  const items = buildDotMenuItems(snapshot)

  return (
    <OverlaySurfaceFrame>
      <div
        data-testid="dot-radial-stage"
        className="pointer-events-auto absolute inset-0 flex items-center justify-center"
        onMouseLeave={() => {
          void fetch('/overlay/radial/hide', { method: 'POST' })
        }}
      >
        <div
          data-testid="dot-radial-shell"
          data-overlay-surface="dot-radial"
          data-overlay-mode="circle"
          data-overlay-state={snapshot.state}
          data-overlay-phase={snapshot.phase}
          data-active-mode={snapshot.activeMode}
          data-overlay-size={tone.size}
          data-overlay-color={tone.color}
          aria-label={overlayStatusLabel(snapshot)}
          className="relative flex items-center justify-center"
        >
          <span data-testid="dot-radial-status" className="sr-only">
            {overlayStatusLabel(snapshot)}
          </span>
          <div className="sr-only" data-testid="dot-radial-item-labels">
            {items.map((item) => (
              <span key={item.id}>{item.label}</span>
            ))}
          </div>
          <div className="relative flex items-center justify-center">
            <DotRadialMenu
              screenEdge={snapshot.position}
              items={items}
              open
              onClose={() => void 0}
            />
          </div>
        </div>
      </div>
    </OverlaySurfaceFrame>
  )
}

export function PillAnchorOverlay() {
  const snapshot = useOverlaySnapshot()

  if (!snapshot.visible) {
    return null
  }

  return <PillAnchorOverlayView snapshot={snapshot} />
}

function buildDotMenuItems(snapshot: SpeechKitOverlayState): DotMenuItem[] {
  const modeItems = MODE_ORDER.map((mode) => ({
    id: mode,
    label: MODE_META[mode].label,
    icon: MODE_META[mode].icon,
    pressed: modeEnabled(snapshot, mode),
    runtimeActive: snapshot.activeMode === mode,
    slashed: !modeEnabled(snapshot, mode) || !modeAvailable(snapshot, mode),
    onClick: () => {
      toggleMode(mode, snapshot)
    },
  }))

  return [
    {
      id: 'copy',
      label: 'Copy',
      icon: ClipboardCopy,
      onClick: () => void navigator.clipboard.writeText(snapshot.lastTranscription),
    },
    {
      id: 'note',
      label: 'Note',
      icon: FileText,
      onClick: () => {
        void fetch('/quicknotes/open-capture', { method: 'POST' })
      },
    },
    ...modeItems,
    {
      id: 'mic',
      label: 'Microphone',
      icon: AudioLines,
      onClick: () => {
        void fetch('/overlay/show-dashboard', { method: 'POST' })
      },
    },
  ]
}

export function DotRadialOverlay() {
  const snapshot = useOverlaySnapshot()

  if (!snapshot.visible) {
    return null
  }

  return <DotRadialOverlayView snapshot={snapshot} />
}

export function DotAnchorOverlay() {
  const snapshot = useOverlaySnapshot()

  if (!snapshot.visible) {
    return null
  }

  return <DotAnchorOverlayView snapshot={snapshot} />
}

export function PillPanelOverlay() {
  const snapshot = useOverlaySnapshot()

  if (!snapshot.visible) {
    return null
  }

  return <PillPanelOverlayView snapshot={snapshot} />
}

export function OverlayApp() {
  const snapshot = useOverlaySnapshot()

  if (!snapshot.visible) {
    return null
  }

  return snapshot.visualizer === 'circle'
    ? <DotAnchorOverlayView snapshot={snapshot} />
    : <PillAnchorOverlayView snapshot={snapshot} />
}

export { PillPanelOverlay as PillActionsOverlay }

function overlayStatusLabel(snapshot: SpeechKitOverlayState): string {
  const mode = snapshot.state === 'idle'
    ? modeStatusLabel('none')
    : modeStatusLabel(snapshot.activeMode)
  const text = snapshot.text.trim()
  if (text) {
    return `${mode} ${decapitalize(text)}`
  }

  switch (snapshot.phase) {
    case 'listening':
      return `${mode} listening`
    case 'speaking':
      return `${mode} speaking`
    case 'thinking':
      return `${mode} thinking`
    case 'done':
      return `${mode} done`
    default:
      return `${mode} ready`
  }
}

function decapitalize(value: string) {
  if (!value) {
    return value
  }
  return value.charAt(0).toLowerCase() + value.slice(1)
}

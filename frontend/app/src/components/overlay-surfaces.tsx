import type { ReactNode } from 'react'
import { ClipboardCopy, FileText, Bot, Mic, AudioLines } from 'lucide-react'

import { AgentAudioVisualizerBar } from '@/components/agent-audio-visualizer-bar'
import { AgentAudioVisualizerRadial } from '@/components/agent-audio-visualizer-radial'
import { DotRadialMenu, type DotMenuItem } from '@/components/dot-radial-menu'
import {
  resolveOverlayTone,
  type OverlayTone,
} from '@/components/overlay-tone'
import { useOverlaySnapshot } from '@/hooks/use-overlay-snapshot'
import {
  setActiveMode,
  type RuntimeMode,
  type SpeechKitOverlayState,
} from '@/lib/speechkit'

function OverlaySurfaceFrame({
  children,
}: {
  children: ReactNode
}) {
  return <div className="pointer-events-none relative h-screen w-screen overflow-visible">{children}</div>
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
  active,
  onClick,
}: {
  label: string
  icon: ReactNode
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className={[
        'flex h-6 w-6 items-center justify-center rounded-full border transition-colors',
        active
          ? 'border-orange-400/35 bg-orange-500/20 text-orange-100'
          : 'border-white/10 bg-white/6 text-white/55 hover:border-white/18 hover:text-white/80',
      ].join(' ')}
    >
      {icon}
    </button>
  )
}

function BubbleGlyph() {
  return (
    <svg
      data-testid="dot-anchor-glyph"
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="pointer-events-none absolute left-1/2 top-1/2 z-0 h-[11px] w-[11px] -translate-x-1/2 -translate-y-1/2 opacity-45"
    >
      <path
        d="M12 2.5C7.03 2.5 3 6.08 3 10.5c0 2.18 1 4.16 2.63 5.6-.17 1.47-.78 3.08-1.88 4.4 2.35-.16 4.2-.93 5.54-2.02.83.21 1.7.32 2.71.32 4.97 0 9-3.58 9-8S16.97 2.5 12 2.5Z"
        fill="currentColor"
      />
    </svg>
  )
}

function OverlayPillShell({
  snapshot,
  tone,
  shellClassName,
  surface,
  children,
}: {
  snapshot: SpeechKitOverlayState
  tone: OverlayTone
  shellClassName: string
  surface: string
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
      className={[
        'relative flex items-center justify-center transition-all duration-200 ease-out',
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
            className="pointer-events-none absolute left-1/2 top-1/2 z-0 h-[8px] w-auto -translate-x-1/2 -translate-y-1/2 opacity-55"
          />
        ) : null}
      </div>
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
  const copyLast = () => {
    if (snapshot.lastTranscription) {
      void navigator.clipboard.writeText(snapshot.lastTranscription)
    }
  }

  const openQuickCapture = () => {
    void fetch('/quicknotes/open-capture', { method: 'POST' })
  }

  const switchMode = (mode: RuntimeMode) => {
    if (snapshot.activeMode === mode) {
      return
    }
    void setActiveMode(mode)
  }

  return (
    <OverlaySurfaceFrame>
      <div
        data-testid="pill-panel-stage"
        className="pointer-events-auto absolute inset-0 flex items-center justify-center"
        onMouseLeave={() => {
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
          className="relative flex items-center gap-1 rounded-full bg-neutral-950/84 px-1.5 py-0.5 shadow-[0_14px_28px_rgba(0,0,0,0.28)] backdrop-blur-xl"
        >
          <span data-testid="pill-panel-status" className="sr-only">
            {overlayStatusLabel(snapshot)}
          </span>
          <div className="flex items-center gap-0.5">
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
          </div>

          <OverlayPillShell
            snapshot={snapshot}
            tone={tone}
            shellClassName={tone.shellClassName}
            surface="pill-panel-center"
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

          <div className="flex items-center gap-0.5">
            <OverlayModeAction
              label={snapshot.activeMode === 'agent' ? 'Switch to Dictate' : 'Switch to Agent'}
              icon={snapshot.activeMode === 'agent' ? <Mic className="h-3 w-3" /> : <Bot className="h-3 w-3" />}
              active={false}
              onClick={() => switchMode(snapshot.activeMode === 'agent' ? 'dictate' : 'agent')}
            />
            <OverlayModeAction
              label="Microphone settings"
              icon={<AudioLines className="h-3 w-3" />}
              active={false}
              onClick={() => { void fetch('/overlay/show-dashboard', { method: 'POST' }) }}
            />
          </div>
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
  const toggleMode = snapshot.activeMode === 'agent' ? 'dictate' : 'agent'
  const modeLabel = snapshot.activeMode === 'agent' ? 'Dictate' : 'Agent'

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
    {
      id: 'mode',
      label: modeLabel,
      icon: snapshot.activeMode === 'agent' ? Mic : Bot,
      onClick: () => {
        void setActiveMode(toggleMode)
      },
    },
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
  const mode = snapshot.activeMode === 'agent' ? 'Agent' : 'Dictate'
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

function decapitalize(value: string): string {
  if (!value) {
    return value
  }
  return value.charAt(0).toLowerCase() + value.slice(1)
}

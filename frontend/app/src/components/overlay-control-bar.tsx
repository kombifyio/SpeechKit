'use client'

import { ClipboardCopy, FileText, Bot, Mic } from 'lucide-react'

interface OverlayControlBarProps {
  onCopy: () => void
  onNote: () => void
  onAgent?: () => void
  visible: boolean
}

export function OverlayControlBar({ onCopy, onNote, onAgent, visible }: OverlayControlBarProps) {
  return (
    <div
      className="pointer-events-auto inline-flex items-center gap-1 rounded-full bg-neutral-900 border border-white/15 px-2 py-1 transition-all duration-200 ease-out"
      style={{
        opacity: visible ? 1 : 0,
        transform: visible ? 'scale(1)' : 'scale(0.9)',
        pointerEvents: visible ? 'auto' : 'none',
      }}
    >
      <button
        onClick={onCopy}
        className="h-8 w-8 rounded-full flex items-center justify-center text-white/80 hover:bg-white/15 active:bg-white/25 transition-colors"
        title="Copy"
      >
        <ClipboardCopy className="h-4 w-4" />
      </button>

      <button
        onClick={onNote}
        className="h-8 w-8 rounded-full flex items-center justify-center text-white/80 hover:bg-white/15 active:bg-white/25 transition-colors"
        title="Note"
      >
        <FileText className="h-4 w-4" />
      </button>

      <button
        onClick={onAgent}
        disabled
        className="h-8 w-8 rounded-full flex items-center justify-center text-white/80 opacity-30 cursor-not-allowed transition-colors"
        title="Agent (coming soon)"
      >
        <Bot className="h-4 w-4" />
      </button>

      <button
        disabled
        className="h-8 w-8 rounded-full flex items-center justify-center text-white/80 opacity-30 cursor-not-allowed transition-colors"
        title="Mic (coming soon)"
      >
        <Mic className="h-4 w-4" />
      </button>
    </div>
  )
}

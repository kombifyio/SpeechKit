import type { SpeechKitOverlayState } from '@/lib/speechkit'

const LEVEL_RISE_FACTOR = 0.7
const LEVEL_DECAY_PER_SECOND = 1.2
const SPEAKING_HOLD_THRESHOLD = 0.14

export function smoothOverlaySnapshot(
  previous: SpeechKitOverlayState,
  next: SpeechKitOverlayState,
  elapsedMs: number,
): SpeechKitOverlayState {
  const elapsedSeconds = Math.max(elapsedMs, 0) / 1000
  const targetLevel = clampLevel(next.level)
  const previousLevel = clampLevel(previous.level)

  let level = targetLevel
  if (targetLevel > previousLevel) {
    level = previousLevel + (targetLevel - previousLevel) * LEVEL_RISE_FACTOR
  } else if (targetLevel < previousLevel) {
    level = Math.max(targetLevel, previousLevel - elapsedSeconds * LEVEL_DECAY_PER_SECOND)
  }

  let phase = next.phase
  if (
    next.state === 'recording' &&
    next.phase === 'listening' &&
    previous.phase === 'speaking' &&
    level > SPEAKING_HOLD_THRESHOLD
  ) {
    phase = 'speaking'
  }

  return {
    ...next,
    level,
    phase,
  }
}

function clampLevel(level: number) {
  return Math.min(Math.max(level, 0), 1)
}

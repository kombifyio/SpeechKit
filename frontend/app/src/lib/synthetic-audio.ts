function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

export function buildSyntheticBands(count: number, level: number) {
  const safeLevel = clamp(level, 0, 1)
  const floor = safeLevel > 0 ? 0.08 : 0

  return Array.from({ length: count }, (_, index) => {
    const angle = (index / Math.max(count, 1)) * Math.PI * 2
    const wave = 0.62 + 0.38 * Math.sin(angle + safeLevel * Math.PI * 1.5)
    return clamp(floor + safeLevel * wave, 0, 1)
  })
}

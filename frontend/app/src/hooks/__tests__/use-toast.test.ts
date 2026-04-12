import { describe, expect, it, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useToast } from '../use-toast'

describe('useToast', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  it('adds and auto-dismisses toasts', () => {
    const { result } = renderHook(() => useToast())

    act(() => result.current.addToast('hello', 'success'))
    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0].message).toBe('hello')
    expect(result.current.toasts[0].type).toBe('success')

    act(() => vi.advanceTimersByTime(5001))
    expect(result.current.toasts).toHaveLength(0)
  })

  it('keeps max 5 toasts', () => {
    const { result } = renderHook(() => useToast())

    act(() => {
      for (let i = 0; i < 8; i++) {
        result.current.addToast(`msg-${i}`)
      }
    })
    expect(result.current.toasts.length).toBeLessThanOrEqual(5)
  })

  it('dismisses a specific toast', () => {
    const { result } = renderHook(() => useToast())

    act(() => result.current.addToast('a'))
    act(() => result.current.addToast('b'))
    const id = result.current.toasts[0].id

    act(() => result.current.dismissToast(id))
    expect(result.current.toasts.every(t => t.id !== id)).toBe(true)
  })
})

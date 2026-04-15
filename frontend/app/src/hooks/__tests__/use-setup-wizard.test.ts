import { describe, expect, it, vi, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'

import { useSetupWizard } from '../use-setup-wizard'

vi.mock('@/lib/speechkit', () => ({
  fetchAudioDevices: vi.fn().mockResolvedValue({
    devices: [
      { deviceId: 'mic-1', label: 'Built-in Mic', isDefault: true },
      { deviceId: 'mic-2', label: 'USB Mic', isDefault: false },
    ],
    selectedDeviceId: 'mic-1',
  }),
  setAudioDevice: vi.fn().mockResolvedValue('ok'),
}))

vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) }))

describe('useSetupWizard', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('starts at welcome step with devices loaded', async () => {
    const onComplete = vi.fn()
    const { result } = renderHook(() => useSetupWizard(onComplete))

    expect(result.current.step).toBe('welcome')
    await waitFor(() => expect(result.current.devices).toHaveLength(2))
    expect(result.current.selectedDevice).toBe('mic-1')
  })

  it('navigates through steps', async () => {
    const onComplete = vi.fn()
    const { result } = renderHook(() => useSetupWizard(onComplete))

    act(() => result.current.setStep('microphone'))
    expect(result.current.step).toBe('microphone')

    act(() => result.current.setStep('hotkey'))
    expect(result.current.step).toBe('hotkey')

    act(() => result.current.setHotkey('ctrl+shift+d'))
    expect(result.current.hotkey).toBe('ctrl+shift+d')
  })

  it('calls onComplete when finishing', async () => {
    const onComplete = vi.fn()
    const { result } = renderHook(() => useSetupWizard(onComplete))

    await act(() => result.current.finish())
    expect(onComplete).toHaveBeenCalled()
  })
})

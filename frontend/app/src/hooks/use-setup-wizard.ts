import { useCallback, useEffect, useState } from 'react'

import {
  builtInPrimaryModelSelections,
  fetchAudioDevices,
  setAudioDevice,
  type AudioDevice,
} from '@/lib/speechkit'

export type WizardStep = 'welcome' | 'microphone' | 'hotkey' | 'done'

export function useSetupWizard(onComplete: () => void) {
  const [step, setStep] = useState<WizardStep>('welcome')
  const [devices, setDevices] = useState<AudioDevice[]>([])
  const [selectedDevice, setSelectedDevice] = useState('')
  const [hotkey, setHotkey] = useState('win+alt')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    void fetchAudioDevices()
      .then((res) => {
        setDevices(res.devices)
        setSelectedDevice(
          res.selectedDeviceId || res.devices[0]?.deviceId || '',
        )
      })
      .catch(() => {})
  }, [])

  const selectDevice = useCallback((deviceId: string) => {
    setSelectedDevice(deviceId)
    void setAudioDevice(deviceId).catch(() => {})
  }, [])

  const finish = useCallback(async () => {
    setLoading(true)
    try {
      const body = new URLSearchParams()
      body.set('dictate_hotkey', hotkey)
      body.set('audio_device_id', selectedDevice)
      body.set(
        'dictate_primary_profile_id',
        builtInPrimaryModelSelections.dictate.primaryProfileId,
      )
      body.set(
        'assist_primary_profile_id',
        builtInPrimaryModelSelections.assist.primaryProfileId,
      )
      body.set(
        'voice_primary_profile_id',
        builtInPrimaryModelSelections.voice_agent.primaryProfileId,
      )
      await fetch('/settings/update', { method: 'POST', body })
    } catch { /* ignore */ }
    onComplete()
  }, [hotkey, selectedDevice, onComplete])

  return {
    step,
    setStep,
    devices,
    selectedDevice,
    selectDevice,
    hotkey,
    setHotkey,
    loading,
    finish,
  }
}

import { useCallback, useEffect, useState } from 'react'

import {
  fetchAudioDevices,
  type AudioDevice,
  type AudioDevicesResponse,
  setAudioDevice,
} from '@/lib/speechkit'

async function loadBrowserAudioDevices(): Promise<AudioDevicesResponse> {
  const deviceList = await navigator.mediaDevices.enumerateDevices()
  const audioInputs = deviceList
    .filter((device) => device.kind === 'audioinput')
    .map((device) => {
      let cleanLabel = device.label || `Microphone ${device.deviceId.slice(0, 8)}`
      cleanLabel = cleanLabel.replace(/\s*\([^)]*\)/g, '').trim()

      return {
        deviceId: device.deviceId,
        label: cleanLabel,
        groupId: device.groupId,
        isDefault: device.deviceId === 'default',
      } satisfies AudioDevice
    })

  return {
    devices: audioInputs,
    selectedDeviceId: audioInputs.find((device) => device.isDefault)?.deviceId ?? audioInputs[0]?.deviceId ?? '',
  }
}

export function useAudioDevices() {
  const [devices, setDevices] = useState<AudioDevice[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [hasPermission, setHasPermission] = useState(false)

  const refreshDevices = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)

      const result = await fetchAudioDevices()
      setDevices(result.devices)
      setSelectedDeviceId(result.selectedDeviceId)
      setHasPermission(true)
    } catch (backendError) {
      try {
        if (!navigator.mediaDevices?.enumerateDevices) {
          throw backendError
        }
        const result = await loadBrowserAudioDevices()
        setDevices(result.devices)
        setSelectedDeviceId(result.selectedDeviceId)
        setHasPermission(false)
      } catch (browserError) {
        setError(
          browserError instanceof Error
            ? browserError.message
            : 'Failed to get audio devices',
        )
        console.error('Error getting audio devices:', browserError)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  const loadDevicesWithPermission = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)

      if (navigator.mediaDevices?.getUserMedia) {
        const tempStream = await navigator.mediaDevices.getUserMedia({
          audio: true,
        })
        tempStream.getTracks().forEach((track) => track.stop())
      }

      const result = await fetchAudioDevices()
      setDevices(result.devices)
      setSelectedDeviceId(result.selectedDeviceId)
      setHasPermission(true)
    } catch (backendError) {
      try {
        if (!navigator.mediaDevices?.enumerateDevices) {
          throw backendError
        }
        const result = await loadBrowserAudioDevices()
        setDevices(result.devices)
        setSelectedDeviceId(result.selectedDeviceId)
        setHasPermission(true)
      } catch (browserError) {
        setError(
          browserError instanceof Error
            ? browserError.message
            : 'Failed to get audio devices',
        )
        console.error('Error getting audio devices:', browserError)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  const selectDevice = useCallback(async (deviceId: string) => {
    setSelectedDeviceId(deviceId)
    try {
      await setAudioDevice(deviceId)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to set audio device')
      throw err
    }
  }, [])

  useEffect(() => {
    void refreshDevices()
  }, [refreshDevices])

  useEffect(() => {
    if (!navigator.mediaDevices?.addEventListener) {
      return
    }

    const handleDeviceChange = () => {
      if (hasPermission) {
        void loadDevicesWithPermission()
      } else {
        void refreshDevices()
      }
    }

    navigator.mediaDevices.addEventListener('devicechange', handleDeviceChange)

    return () => {
      navigator.mediaDevices.removeEventListener(
        'devicechange',
        handleDeviceChange,
      )
    }
  }, [hasPermission, loadDevicesWithPermission, refreshDevices])

  return {
    devices,
    selectedDeviceId,
    loading,
    error,
    hasPermission,
    loadDevices: loadDevicesWithPermission,
    refreshDevices,
    setSelectedDevice: selectDevice,
  }
}

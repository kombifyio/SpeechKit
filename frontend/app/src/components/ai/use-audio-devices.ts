import { useCallback, useEffect, useState } from "react"

export const useAudioDevices = () => {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [hasPermission, setHasPermission] = useState(false)

  const loadDevicesWithoutPermission = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)

      const deviceList = await navigator.mediaDevices.enumerateDevices()
      const audioInputs = deviceList.filter(device => device.kind === "audioinput")

      setDevices(audioInputs)
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to get audio devices"
      setError(message)
      console.error("Error getting audio devices:", message)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadDevicesWithPermission = useCallback(async () => {
    if (loading) {
      return
    }

    try {
      setLoading(true)
      setError(null)

      const tempStream = await navigator.mediaDevices.getUserMedia({ audio: true })

      for (const track of tempStream.getTracks()) {
        track.stop()
      }

      const deviceList = await navigator.mediaDevices.enumerateDevices()
      const audioInputs = deviceList.filter(device => device.kind === "audioinput")

      setDevices(audioInputs)
      setHasPermission(true)
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to get audio devices"
      setError(message)
      console.error("Error getting audio devices:", message)
    } finally {
      setLoading(false)
    }
  }, [loading])

  useEffect(() => {
    loadDevicesWithoutPermission()
  }, [loadDevicesWithoutPermission])

  useEffect(() => {
    const handleDeviceChange = () => {
      if (hasPermission) {
        loadDevicesWithPermission()
      } else {
        loadDevicesWithoutPermission()
      }
    }

    navigator.mediaDevices.addEventListener("devicechange", handleDeviceChange)

    return () => {
      navigator.mediaDevices.removeEventListener("devicechange", handleDeviceChange)
    }
  }, [hasPermission, loadDevicesWithPermission, loadDevicesWithoutPermission])

  return {
    devices,
    loading,
    error,
    hasPermission,
    loadDevices: loadDevicesWithPermission,
  }
}

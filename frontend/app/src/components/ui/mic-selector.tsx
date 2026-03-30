"use client"

import { useEffect, useState } from "react"
import { Check, ChevronsUpDown, Mic, MicOff } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { useAudioDevices } from "@/components/ui/use-audio-devices"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { LiveWaveform } from "@/components/ui/live-waveform"

export interface MicSelectorProps {
  value?: string
  onValueChange?: (deviceId: string) => void
  muted?: boolean
  onMutedChange?: (muted: boolean) => void
  disabled?: boolean
  compact?: boolean
  className?: string
}

export function MicSelector({
  value,
  onValueChange,
  muted,
  onMutedChange,
  disabled,
  compact,
  className,
}: MicSelectorProps) {
  const {
    devices,
    selectedDeviceId,
    loading,
    error,
    hasPermission,
    loadDevices,
    setSelectedDevice,
  } = useAudioDevices()
  const [uncontrolledSelectedDevice, setUncontrolledSelectedDevice] = useState<string>(value || "")
  const [internalMuted, setInternalMuted] = useState(false)
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)

  // Use controlled muted if provided, otherwise use internal state
  const isMuted = muted !== undefined ? muted : internalMuted

  // Select first device by default
  const defaultDeviceId = devices[0]?.deviceId || ""
  const selectedDevice = (value ?? uncontrolledSelectedDevice) || selectedDeviceId || defaultDeviceId
  useEffect(() => {
    if (value === undefined && !uncontrolledSelectedDevice && defaultDeviceId) {
      onValueChange?.(defaultDeviceId)
    }
  }, [defaultDeviceId, onValueChange, uncontrolledSelectedDevice, value])

  const currentDevice = devices.find((d) => d.deviceId === selectedDevice) ||
    devices[0] || {
      label: loading ? "Loading..." : "No microphone",
      deviceId: "",
    }

  const handleDeviceSelect = (deviceId: string, e?: React.MouseEvent) => {
    e?.preventDefault()
    if (value === undefined) {
      setUncontrolledSelectedDevice(deviceId)
    }
    void setSelectedDevice(deviceId).catch(() => undefined)
    onValueChange?.(deviceId)
  }

  const handleDropdownOpenChange = async (open: boolean) => {
    setIsDropdownOpen(open)
    if (open && !hasPermission && !loading) {
      await loadDevices()
    }
  }

  const toggleMute = () => {
    const newMuted = !isMuted
    if (muted === undefined) {
      setInternalMuted(newMuted)
    }
    onMutedChange?.(newMuted)
  }

  const isPreviewActive = isDropdownOpen && !isMuted

  return (
    <DropdownMenu onOpenChange={handleDropdownOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size={compact ? 'icon-sm' : 'sm'}
          className={cn(
            compact
              ? 'hover:bg-accent flex cursor-pointer items-center justify-center gap-1.5 px-0'
              : 'hover:bg-accent flex w-48 cursor-pointer items-center gap-1.5',
            className
          )}
          disabled={loading || disabled}
          aria-label={
            compact
              ? `Microphone ${currentDevice.label}`
              : `Microphone ${currentDevice.label}`
          }
        >
          {isMuted ? (
            <MicOff className="h-4 w-4 flex-shrink-0" />
          ) : (
            <Mic className="h-4 w-4 flex-shrink-0" />
          )}
          {!compact ? (
            <>
              <span className="flex-1 truncate text-left">
                {currentDevice.label}
              </span>
              <ChevronsUpDown className="h-3 w-3 flex-shrink-0" />
            </>
          ) : null}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="center" side="top" className="w-72">
        {loading ? (
          <DropdownMenuItem disabled>Loading devices...</DropdownMenuItem>
        ) : error ? (
          <DropdownMenuItem disabled>Error: {error}</DropdownMenuItem>
        ) : (
          devices.map((device) => (
            <DropdownMenuItem
              key={device.deviceId}
              onClick={(e) => handleDeviceSelect(device.deviceId, e)}
              onSelect={(e) => e.preventDefault()}
              className="flex items-center justify-between"
            >
              <span className="truncate">{device.label}</span>
              {selectedDevice === device.deviceId && (
                <Check className="h-4 w-4 flex-shrink-0" />
              )}
            </DropdownMenuItem>
          ))
        )}
        {devices.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <div className="flex items-center gap-2 p-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={(e) => {
                  e.preventDefault()
                  toggleMute()
                }}
                className="h-8 gap-2"
              >
                {isMuted ? (
                  <MicOff className="h-4 w-4" />
                ) : (
                  <Mic className="h-4 w-4" />
                )}
                <span className="text-sm">{isMuted ? "Unmute" : "Mute"}</span>
              </Button>
              <div className="bg-accent ml-auto w-16 overflow-hidden rounded-md p-1.5">
                <LiveWaveform
                  active={isPreviewActive}
                  deviceId={selectedDevice || defaultDeviceId}
                  mode="static"
                  height={15}
                  barWidth={3}
                  barGap={1}
                />
              </div>
            </div>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

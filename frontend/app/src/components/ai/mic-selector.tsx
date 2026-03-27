import { useControllableState } from "@radix-ui/react-use-controllable-state"
import { ChevronsUpDownIcon, MicIcon } from "lucide-react"
import {
  type ComponentProps,
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import { useAudioDevices } from "./use-audio-devices"

const deviceIdRegex = /\(([\da-fA-F]{4}:[\da-fA-F]{4})\)$/

interface MicSelectorContextType {
  data: MediaDeviceInfo[]
  value: string | undefined
  onValueChange?: (value: string) => void
  open: boolean
  onOpenChange?: (open: boolean) => void
  width: number
  setWidth?: (width: number) => void
}

const MicSelectorContext = createContext<MicSelectorContextType>({
  data: [],
  value: undefined,
  onValueChange: undefined,
  open: false,
  onOpenChange: undefined,
  width: 200,
  setWidth: undefined,
})

export type MicSelectorProps = ComponentProps<typeof Popover> & {
  defaultValue?: string
  value?: string | undefined
  onValueChange?: (value: string | undefined) => void
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

export const MicSelector = ({
  defaultValue,
  value: controlledValue,
  onValueChange: controlledOnValueChange,
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  ...props
}: MicSelectorProps) => {
  const [value, onValueChange] = useControllableState<string | undefined>({
    defaultProp: defaultValue,
    prop: controlledValue,
    onChange: controlledOnValueChange,
  })
  const [open, onOpenChange] = useControllableState({
    defaultProp: defaultOpen,
    prop: controlledOpen,
    onChange: controlledOnOpenChange,
  })
  const [width, setWidth] = useState(200)
  const { devices, loading, hasPermission, loadDevices } = useAudioDevices()

  useEffect(() => {
    if (open && !hasPermission && !loading) {
      loadDevices()
    }
  }, [open, hasPermission, loading, loadDevices])

  return (
    <MicSelectorContext.Provider
      value={{
        data: devices,
        value,
        onValueChange,
        open: open ?? false,
        onOpenChange,
        width,
        setWidth,
      }}
    >
      <Popover {...props} onOpenChange={onOpenChange} open={open} />
    </MicSelectorContext.Provider>
  )
}

export type MicSelectorTriggerProps = ComponentProps<typeof Button>

export const MicSelectorTrigger = ({ children, ...props }: MicSelectorTriggerProps) => {
  const { setWidth } = useContext(MicSelectorContext)
  const ref = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const resizeObserver = new ResizeObserver(entries => {
      for (const entry of entries) {
        const newWidth = (entry.target as HTMLElement).offsetWidth
        if (newWidth) {
          setWidth?.(newWidth)
        }
      }
    })

    if (ref.current) {
      resizeObserver.observe(ref.current)
    }

    return () => {
      resizeObserver.disconnect()
    }
  }, [setWidth])

  return (
    <PopoverTrigger asChild>
      <Button variant="outline" {...props} ref={ref}>
        {children}
        <ChevronsUpDownIcon className="shrink-0 text-muted-foreground" size={16} />
      </Button>
    </PopoverTrigger>
  )
}

export type MicSelectorContentProps = ComponentProps<typeof Command> & {
  popoverOptions?: ComponentProps<typeof PopoverContent>
}

export const MicSelectorContent = ({
  className,
  popoverOptions,
  ...props
}: MicSelectorContentProps) => {
  const { width, onValueChange, value } = useContext(MicSelectorContext)

  return (
    <PopoverContent className={cn("p-0", className)} style={{ width }} {...popoverOptions}>
      <Command onValueChange={onValueChange} value={value} {...props} />
    </PopoverContent>
  )
}

export type MicSelectorInputProps = ComponentProps<typeof CommandInput>

export const MicSelectorInput = ({ ...props }: MicSelectorInputProps) => (
  <CommandInput placeholder="Search microphones..." {...props} />
)

export type MicSelectorListProps = Omit<ComponentProps<typeof CommandList>, "children"> & {
  children: (devices: MediaDeviceInfo[]) => ReactNode
}

export const MicSelectorList = ({ children, ...props }: MicSelectorListProps) => {
  const { data } = useContext(MicSelectorContext)

  return <CommandList {...props}>{children(data)}</CommandList>
}

export type MicSelectorEmptyProps = ComponentProps<typeof CommandEmpty>

export const MicSelectorEmpty = ({
  children = "No microphone found.",
  ...props
}: MicSelectorEmptyProps) => <CommandEmpty {...props}>{children}</CommandEmpty>

export type MicSelectorItemProps = ComponentProps<typeof CommandItem>

export const MicSelectorItem = (props: MicSelectorItemProps) => {
  const { onValueChange, onOpenChange } = useContext(MicSelectorContext)

  return (
      <CommandItem
        onSelect={(currentValue: string) => {
          onValueChange?.(currentValue)
          onOpenChange?.(false)
        }}
        {...props}
      />
  )
}

export type MicSelectorLabelProps = ComponentProps<"span"> & {
  device: MediaDeviceInfo
}

export const MicSelectorLabel = ({ device, className, ...props }: MicSelectorLabelProps) => {
  const matches = device.label.match(deviceIdRegex)

  if (!matches) {
    return (
      <span className={className} {...props}>
        {device.label || "Unknown Microphone"}
      </span>
    )
  }

  const [, deviceId] = matches
  const name = device.label.replace(deviceIdRegex, "")

  return (
    <span className={className} {...props}>
      <span>{name}</span>
      <span className="text-muted-foreground"> ({deviceId})</span>
    </span>
  )
}

export type MicSelectorValueProps = ComponentProps<"span">

export const MicSelectorValue = ({ className, ...props }: MicSelectorValueProps) => {
  const { data, value } = useContext(MicSelectorContext)
  const currentDevice = data.find(d => d.deviceId === value)

  if (!currentDevice) {
    return (
      <span className={cn("flex-1 text-left", className)} {...props}>
        Select microphone...
      </span>
    )
  }

  return (
    <MicSelectorLabel
      className={cn("flex-1 text-left", className)}
      device={currentDevice}
      {...props}
    />
  )
}

/** Demo component for preview */
export default function MicSelectorDemo() {
  const [selectedMic, setSelectedMic] = useState<string | undefined>()

  return (
    <div className="w-full max-w-sm p-4">
      <MicSelector value={selectedMic} onValueChange={setSelectedMic}>
        <MicSelectorTrigger className="w-full justify-between">
          <div className="flex items-center gap-2">
            <MicIcon className="size-4" />
            <MicSelectorValue />
          </div>
        </MicSelectorTrigger>
        <MicSelectorContent>
          <MicSelectorInput />
          <MicSelectorList>
            {devices => (
              <>
                <MicSelectorEmpty />
                {devices.map(device => (
                  <MicSelectorItem key={device.deviceId} value={device.deviceId}>
                    <MicSelectorLabel device={device} />
                  </MicSelectorItem>
                ))}
              </>
            )}
          </MicSelectorList>
        </MicSelectorContent>
      </MicSelector>
    </div>
  )
}

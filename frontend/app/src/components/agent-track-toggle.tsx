import { Fragment, type ComponentProps, useMemo, useState } from 'react';
import { type VariantProps } from 'class-variance-authority';
import { Track } from 'livekit-client';
import {
  MicIcon,
  MicOffIcon,
  MonitorUpIcon,
  MonitorOffIcon,
  LoaderIcon,
  VideoIcon,
  VideoOffIcon,
} from 'lucide-react';
import { agentTrackToggleVariants } from '@/components/agent-track-toggle.styles';
import { Toggle } from '@/components/ui/toggle';
import { cn } from '@/lib/utils';

function renderSourceIcon(source: Track.Source, enabled: boolean, pending = false, className?: string) {
  if (pending) {
    return <LoaderIcon className={className} />;
  }

  switch (source) {
    case Track.Source.Microphone:
      return enabled ? <MicIcon className={className} /> : <MicOffIcon className={className} />;
    case Track.Source.Camera:
      return enabled ? <VideoIcon className={className} /> : <VideoOffIcon className={className} />;
    case Track.Source.ScreenShare:
      return enabled ? <MonitorUpIcon className={className} /> : <MonitorOffIcon className={className} />;
    default:
      return <Fragment />;
  }
}

/**
 * Props for the AgentTrackToggle component.
 */
export type AgentTrackToggleProps = VariantProps<typeof agentTrackToggleVariants> &
  ComponentProps<'button'> & {
    /**
     * The size of the toggle.
     */
    size?: 'sm' | 'default' | 'lg';
    /**
     * The variant of the toggle.
     * @defaultValue 'default'
     */
    variant?: 'default' | 'outline';
    /**
     * The track source to toggle (Microphone, Camera, or ScreenShare).
     */
    source: 'camera' | 'microphone' | 'screen_share';
    /**
     * Whether the toggle is in a pending/loading state.
     * When true, displays a loading spinner icon.
     * @defaultValue false
     */
    pending?: boolean;
    /**
     * Whether the toggle is currently pressed/enabled.
     * @defaultValue false
     */
    pressed?: boolean;
    /**
     * The default pressed state when uncontrolled.
     * @defaultValue false
     */
    defaultPressed?: boolean;
    /**
     * Callback fired when the pressed state changes.
     */
    onPressedChange?: (pressed: boolean) => void;
  };

/**
 * A toggle button for controlling track publishing state.
 * Displays appropriate icons based on the track source and state.
 *
 * @extends ComponentProps<'button'>
 *
 * @example
 * ```tsx
 * <AgentTrackToggle
 *   source={Track.Source.Microphone}
 *   pressed={isMicEnabled}
 *   onPressedChange={(pressed) => setMicEnabled(pressed)}
 * />
 * ```
 */
export function AgentTrackToggle({
  size = 'default',
  variant = 'default',
  source,
  pending = false,
  pressed,
  defaultPressed = false,
  className,
  onPressedChange,
  ...props
}: AgentTrackToggleProps) {
  const [uncontrolledPressed, setUncontrolledPressed] = useState(defaultPressed ?? false);
  const isControlled = pressed !== undefined;
  const resolvedPressed = useMemo(
    () => (isControlled ? pressed : uncontrolledPressed) ?? false,
    [isControlled, pressed, uncontrolledPressed],
  );
  const handlePressedChange = (nextPressed: boolean) => {
    if (!isControlled) {
      setUncontrolledPressed(nextPressed);
    }
    onPressedChange?.(nextPressed);
  };

  return (
    <Toggle
      size={size}
      variant={variant}
      pressed={isControlled ? pressed : undefined}
      defaultPressed={isControlled ? undefined : defaultPressed}
      aria-label={`Toggle ${source}`}
      onPressedChange={handlePressedChange}
      className={cn(
        agentTrackToggleVariants({
          size,
          variant: variant ?? 'default',
          className,
        }),
      )}
      {...props}
    >
      {renderSourceIcon(source as Track.Source, resolvedPressed, pending, cn(pending && 'animate-spin'))}
      {props.children}
    </Toggle>
  );
}

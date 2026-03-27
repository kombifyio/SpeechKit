import { cva } from 'class-variance-authority'

export const agentAudioVisualizerBarElementVariants = cva(
  [
    'rounded-full transition-colors duration-250 ease-linear',
    'bg-current/10 data-[lk-highlighted=true]:bg-current',
  ],
  {
    variants: {
      size: {
        icon: 'w-[4px] min-h-[4px]',
        sm: 'w-[8px] min-h-[8px]',
        md: 'w-[16px] min-h-[16px]',
        lg: 'w-[32px] min-h-[32px]',
        xl: 'w-[64px] min-h-[64px]',
      },
    },
    defaultVariants: {
      size: 'md',
    },
  },
)

export const agentAudioVisualizerBarVariants = cva('relative flex items-center justify-center', {
  variants: {
    size: {
      icon: 'h-[24px] gap-[2px]',
      sm: 'h-[56px] gap-[4px]',
      md: 'h-[112px] gap-[8px]',
      lg: 'h-[224px] gap-[16px]',
      xl: 'h-[448px] gap-[32px]',
    },
  },
  defaultVariants: {
    size: 'md',
  },
})

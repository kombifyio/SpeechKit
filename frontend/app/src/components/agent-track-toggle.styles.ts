import { cva } from 'class-variance-authority'

export const agentTrackToggleVariants = cva(['size-9'], {
  variants: {
    size: {
      default: 'h-9 px-2 min-w-9',
      sm: 'h-8 px-1.5 min-w-8',
      lg: 'h-10 px-2.5 min-w-10',
    },
    variant: {
      default: [
        'data-[state=off]:bg-destructive/10 data-[state=off]:text-destructive',
        'data-[state=off]:hover:bg-destructive/15',
        'data-[state=off]:focus-visible:ring-destructive/30',
        'data-[state=on]:bg-accent data-[state=on]:text-accent-foreground',
        'data-[state=on]:hover:bg-foreground/10',
      ],
      outline: [
        'data-[state=off]:bg-destructive/10 data-[state=off]:text-destructive data-[state=off]:border-destructive/20',
        'data-[state=off]:hover:bg-destructive/15 data-[state=off]:hover:text-destructive',
        'data-[state=off]:focus:text-destructive',
        'data-[state=off]:focus-visible:border-destructive data-[state=off]:focus-visible:ring-destructive/30',
        'data-[state=on]:hover:bg-foreground/10 data-[state=on]:hover:border-foreground/12',
        'dark:data-[state=on]:hover:bg-foreground/10',
      ],
    },
  },
  defaultVariants: {
    variant: 'default',
  },
})

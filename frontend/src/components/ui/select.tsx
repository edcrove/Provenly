import * as React from 'react'

import { cn } from '@/lib/utils'

// Native select styled like the shadcn/ui input (keeps the POC dependency-light). min-w-0 max-w-full: a long option
// never widens its form past its card on a phone (a select's intrinsic width is its longest option).
function NativeSelect({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      data-slot="native-select"
      className={cn(
        'border-input h-9 max-w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:ring-ring/50 focus-visible:ring-[3px]',
        className,
      )}
      {...props}
    />
  )
}

export { NativeSelect }

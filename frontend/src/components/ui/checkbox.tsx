import * as React from 'react'

import { cn } from '@/lib/utils'

function Checkbox({ className, ...props }: Omit<React.ComponentProps<'input'>, 'type'>) {
  return (
    <input
      type="checkbox"
      data-slot="checkbox"
      className={cn('border-input accent-primary size-4 shrink-0 rounded-[4px] border shadow-xs', className)}
      {...props}
    />
  )
}

export { Checkbox }

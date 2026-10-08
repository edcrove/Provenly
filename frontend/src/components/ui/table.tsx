import * as React from 'react'

import { cn } from '@/lib/utils'

/**
 * A table that scrolls sideways when it is wider than its card (phones): the first column stays put so each row keeps
 * its name, and a hint says more columns are to the right.
 */
function Table({ className, ...props }: React.ComponentProps<'table'>) {
  const container = React.useRef<HTMLDivElement>(null)
  const [overflows, setOverflows] = React.useState(false)
  React.useEffect(() => {
    const el = container.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const check = () => setOverflows(el.scrollWidth > el.clientWidth + 1)
    check()
    const observer = new ResizeObserver(check)
    observer.observe(el)
    observer.observe(el.firstElementChild!)
    return () => observer.disconnect()
  }, [])
  return (
    <div className="relative w-full min-w-0">
      <div
        ref={container}
        data-slot="table-container"
        className={cn(
          'w-full overflow-x-auto',
          overflows &&
            '[&_tr>*:first-child]:bg-card [&_tr>*:first-child]:sticky [&_tr>*:first-child]:left-0 [&_tr>*:first-child]:z-10',
        )}
      >
        <table data-slot="table" className={cn('w-full caption-bottom text-sm', className)} {...props} />
      </div>
      {overflows ? (
        <p className="text-muted-foreground mt-1 text-xs" data-testid="table-scroll-hint">
          Scroll sideways for more columns →
        </p>
      ) : null}
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return <thead data-slot="table-header" className={cn('[&_tr]:border-b', className)} {...props} />
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  return <tbody data-slot="table-body" className={cn('[&_tr:last-child]:border-0', className)} {...props} />
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      data-slot="table-row"
      className={cn('hover:bg-muted/50 border-b transition-colors', className)}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        'text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap',
        className,
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return <td data-slot="table-cell" className={cn('p-2 align-middle', className)} {...props} />
}

export { Table, TableBody, TableCell, TableHead, TableHeader, TableRow }

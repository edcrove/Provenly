import { useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'

/**
 * Asks in place before an action that cannot be undone: the question, the action and a way back, with the safe
 * option focused (the deprecate pattern). `trigger` renders what opens it; `onConfirm` gets a callback that closes it.
 */
export function InlineConfirm({
  label,
  question,
  confirmLabel,
  dismissLabel = 'Cancel',
  pending = false,
  onConfirm,
  trigger,
}: {
  label: string
  question: ReactNode
  confirmLabel: string
  dismissLabel?: string
  pending?: boolean
  onConfirm: (close: () => void) => void
  trigger: (open: () => void) => ReactNode
}) {
  const [open, setOpen] = useState(false)
  const close = () => setOpen(false)
  if (!open) return <>{trigger(() => setOpen(true))}</>
  return (
    <span role="group" aria-label={label} className="flex flex-wrap items-center gap-2">
      <span className="text-sm">{question}</span>
      <Button size="sm" variant="destructive" disabled={pending} onClick={() => onConfirm(close)}>
        {confirmLabel}
      </Button>
      <Button size="sm" variant="outline" autoFocus onClick={close}>
        {dismissLabel}
      </Button>
    </span>
  )
}

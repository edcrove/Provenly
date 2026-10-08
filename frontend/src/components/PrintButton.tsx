import { Printer } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { formatDateTime } from '@/lib/format'

/** Prints the page as a report (no header, menu, filters or buttons), and what the printout says about itself. */
export function PrintButton({ what }: { what: string }) {
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => window.print()}>
        <Printer /> Print
      </Button>
      <p className="print-only text-muted-foreground text-xs" data-testid="print-caption">
        Provenly · {what} · printed {formatDateTime(new Date().toISOString())}
      </p>
    </>
  )
}

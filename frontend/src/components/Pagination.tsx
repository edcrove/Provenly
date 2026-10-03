import { useEffect } from 'react'

import { Button } from '@/components/ui/button'
import { plural } from '@/lib/format'

interface Props {
  page: number
  totalPages: number
  totalItems: number
  /** replace: the change corrects the current page (no new history entry). */
  onPageChange: (page: number, replace?: boolean) => void
}

export function Pagination({ page, totalPages, totalItems, onPageChange }: Props) {
  // A page past the end (old link, items removed, edited URL) moves to the last page instead of an empty list.
  useEffect(() => {
    if (totalPages >= 1 && page > totalPages) onPageChange(totalPages, true)
  }, [page, totalPages, onPageChange])
  return (
    <nav aria-label="pagination" className="flex items-center justify-between gap-2 pt-3 text-sm">
      <span className="text-muted-foreground">
        Page {Math.min(page, Math.max(totalPages, 1))} of {Math.max(totalPages, 1)} ·{' '}
        {plural(totalItems, 'item')}
      </span>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
          Previous
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onPageChange(page + 1)}
        >
          Next
        </Button>
      </div>
    </nav>
  )
}

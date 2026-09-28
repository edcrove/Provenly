import { Button } from '@/components/ui/button'

interface Props {
  page: number
  totalPages: number
  totalItems: number
  onPageChange: (page: number) => void
}

export function Pagination({ page, totalPages, totalItems, onPageChange }: Props) {
  return (
    <nav aria-label="pagination" className="flex items-center justify-between gap-2 pt-3 text-sm">
      <span className="text-muted-foreground">
        Page {Math.min(page, Math.max(totalPages, 1))} of {Math.max(totalPages, 1)} · {totalItems} items
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

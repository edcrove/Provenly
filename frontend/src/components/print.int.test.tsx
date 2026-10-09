import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { renderRoute } from '@/test/render'

describe('FE-INT-066 printing a report', () => {
  it.each([
    ['/dashboard', 'Provenly · dashboard of TC · printed'],
    ['/test-runs/7', 'Provenly · test run #7 · printed'],
  ])('FE-INT-066 %s prints itself with a caption that says what and when', async (path, caption) => {
    localStorage.setItem('provenly.project', 'TC')
    const print = vi.spyOn(window, 'print').mockImplementation(() => {})
    const { user: u } = renderRoute(path)
    await u.click(await screen.findByRole('button', { name: 'Print' }))
    expect(print).toHaveBeenCalledOnce()
    expect(screen.getByTestId('print-caption')).toHaveTextContent(caption)
    expect(screen.getByTestId('print-caption')).toHaveClass('print-only')
    print.mockRestore()
  })
})

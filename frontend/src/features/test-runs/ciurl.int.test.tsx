import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const ciUrl = 'https://github.com/acme/shop/actions/runs/9876/attempts/1'

describe('FE-INT-070 a run links to its CI job (Ed, 2026-10-12)', () => {
  it('FE-INT-070 the run page and the list open the CI job in a new tab when CI sent its link', async () => {
    db.runs = [
      testRun({ ciUrl }),
      testRun({ id: 8, externalRunId: 'github:9876:2', runAttempt: 2, pipeline: '' }),
    ]
    renderRoute('/test-runs/7')
    await screen.findByRole('heading', { name: 'Test run #7' })
    const link = screen.getByRole('link', { name: 'Open the CI job' })
    expect(link).toHaveAttribute('href', ciUrl)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText('CI job', { selector: 'dt' }).nextElementSibling).toContainElement(link)

    const list = renderRoute('/test-runs')
    const rows = await screen.findAllByTestId('run-row')
    expect(within(rows[1]).getByRole('link', { name: 'ci' })).toHaveAttribute('href', ciUrl)
    expect(within(rows[0]).queryByTestId('ci-job-link')).toBeNull()
    list.unmount()
  })

  it('FE-INT-070 without a link the run page reads — and a nameless pipeline links as "CI job"', async () => {
    db.runs = [
      testRun({ ciUrl: '', pipeline: 'ci' }),
      testRun({ id: 8, externalRunId: 'github:9876:2', runAttempt: 2, pipeline: '', ciUrl }),
    ]
    renderRoute('/test-runs/7')
    await screen.findByRole('heading', { name: 'Test run #7' })
    expect(screen.queryByTestId('ci-job-link')).toBeNull()
    expect(screen.getByText('CI job', { selector: 'dt' }).nextElementSibling).toHaveTextContent('—')

    const list = renderRoute('/test-runs')
    const rows = await screen.findAllByTestId('run-row')
    expect(within(rows[0]).getByRole('link', { name: 'CI job' })).toHaveAttribute('href', ciUrl)
    list.unmount()
  })
})

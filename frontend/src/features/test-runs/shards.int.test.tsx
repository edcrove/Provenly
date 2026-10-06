import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testResult } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

/** Run 7 split into 3 shards: shard 1 arrived (TC-153), shard 3 too (TC-154), shard 2 has not. */
const shardedRun = (status: 'running' | 'interrupted' | 'completed', received = [1, 3]) => {
  const missing = [1, 2, 3].filter((n) => !received.includes(n))
  db.runs[0] = {
    ...db.runs[0],
    mode: 'sharded',
    executionStatus: status,
    completedAt: status === 'running' ? null : db.runs[0].completedAt,
    shards: { total: 3, received, missing },
  }
  db.results = [
    testResult({ id: 1, testCaseId: 153, testName: 'login', shard: 1 }),
    testResult({ id: 2, testCaseId: 154, testName: 'pay', status: 'failed', shard: 3 }),
  ]
}

describe('FE-INT-049 sharded runs (card #57)', () => {
  it('FE-INT-049 a running sharded run shows the shards it received and waits for', async () => {
    shardedRun('running')
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('run-shards')).toHaveTextContent(
      '2 of 3 shards received · waiting for shard 2',
    )
    expect(screen.getByText('sharded')).toBeInTheDocument()
    expect(await screen.findByTestId('run-pass-rate')).toHaveTextContent('so far')
  })

  it('FE-INT-049 an interrupted sharded run names the shards that never arrived; a complete one only counts', async () => {
    shardedRun('interrupted', [1])
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('run-shards')).toHaveTextContent(
      '1 of 3 shards received · shards 2, 3 never arrived',
    )
    expect(screen.getByTestId('interrupted-run')).toBeInTheDocument()
  })

  it('FE-INT-049 a complete sharded run only counts its shards', async () => {
    shardedRun('completed', [1, 2, 3])
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('run-shards')).toHaveTextContent(/^3 of 3 shards received$/)
  })

  it('FE-INT-049 results tell their shard and filter by it', async () => {
    shardedRun('running')
    const { user, router } = renderRoute('/test-runs/7')
    const table = await screen.findByRole('table', { name: 'Results' })
    expect(await within(table).findByText('shard 1')).toBeInTheDocument()
    expect(within(table).getByText('shard 3')).toBeInTheDocument()

    await user.selectOptions(screen.getByRole('combobox', { name: 'Filter by shard' }), 'Shard 3')
    expect(router.state.location.search).toBe('?shard=3')
    await screen.findByText('pay')
    expect(
      within(screen.getByRole('table', { name: 'Results' })).queryByText('login'),
    ).not.toBeInTheDocument()

    await user.selectOptions(screen.getByRole('combobox', { name: 'Filter by shard' }), 'Shard 2')
    expect(await screen.findByText('No results match the filters.')).toBeInTheDocument()
  })

  it('FE-INT-049 runs without shards show no shard progress, marker or filter', async () => {
    renderRoute('/test-runs/7')
    await screen.findAllByTestId('result-row')
    expect(screen.queryByTestId('run-shards')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Filter by shard' })).not.toBeInTheDocument()
    expect(screen.queryByText(/^shard \d/)).not.toBeInTheDocument()
  })
})

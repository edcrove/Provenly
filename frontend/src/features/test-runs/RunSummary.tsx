import { Link } from 'react-router'

import type { TestRunSummary } from '@/api/client'
import { StatusBadge } from '@/components/StatusBadge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatPercent, plural, sumPercents } from '@/lib/format'
import { resultStatuses, summaryStatuses } from '@/lib/status'

/** Snapshot-based summary: counts, % of expected, % of executed and execution %. */
export function RunSummary({ summary }: { summary: TestRunSummary }) {
  const untested = summary.testCases.filter((c) => c.status === 'untested')
  const flaky = summary.testCases.filter((c) => c.flaky)
  // A share of nothing is not 0%: with no expected or no executed test case the percentage is undefined.
  const ofExpected = (p: number) => (summary.expectedTotal > 0 ? formatPercent(p) : '—')
  const ofExecuted = (p: number) => (summary.executedTotal > 0 ? formatPercent(p) : '—')
  return (
    <div className="grid gap-4">
      <div className="grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
        <div className="rounded-md border p-3">
          <div className="text-muted-foreground">Expected (snapshot)</div>
          <div className="text-2xl font-semibold" data-testid="expected-total">
            {summary.expectedTotal}
          </div>
          {summary.amendedTestCaseIds.length > 0 ? (
            <div className="text-muted-foreground text-xs" data-testid="expected-amended">
              {summary.snapshotTotal} in the snapshot + {summary.amendedTestCaseIds.length} included later
            </div>
          ) : null}
        </div>
        <div className="rounded-md border p-3">
          <div className="text-muted-foreground">Executed</div>
          <div className="text-2xl font-semibold" data-testid="executed-total">
            {summary.executedTotal}
          </div>
        </div>
        <div className="rounded-md border p-3">
          <div className="text-muted-foreground">Execution</div>
          <div className="text-2xl font-semibold" data-testid="execution-percent">
            {ofExpected(summary.executionPercent)}
          </div>
          <div className="text-muted-foreground text-xs" data-testid="execution-counts">
            {summary.executedTotal} of {plural(summary.expectedTotal, 'test case')} executed
          </div>
        </div>
        <div className="rounded-md border p-3">
          <div className="text-muted-foreground">Pass rate</div>
          <div className="text-2xl font-semibold" data-testid="pass-rate">
            {ofExecuted(summary.percentOfExecuted.passed)}
          </div>
          <div className="text-muted-foreground text-xs" data-testid="pass-counts">
            {summary.counts.passed} of {summary.executedTotal} executed passed
          </div>
        </div>
      </div>
      <Table aria-label="Summary by status">
        <TableHeader>
          <TableRow>
            <TableHead>Status</TableHead>
            <TableHead>Test cases</TableHead>
            <TableHead>% of expected</TableHead>
            <TableHead>% of executed</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {summaryStatuses.map((s) => (
            <TableRow key={s} data-testid={`summary-${s}`}>
              <TableCell>
                <StatusBadge status={s} />
              </TableCell>
              <TableCell>{summary.counts[s]}</TableCell>
              <TableCell>{ofExpected(summary.percentOfExpected[s])}</TableCell>
              <TableCell>
                {s === 'untested'
                  ? '—'
                  : ofExecuted(summary.percentOfExecuted[s as (typeof resultStatuses)[number]])}
              </TableCell>
            </TableRow>
          ))}
          <TableRow data-testid="summary-total" className="font-medium">
            <TableCell>Total</TableCell>
            <TableCell>{summary.expectedTotal}</TableCell>
            <TableCell>
              {ofExpected(sumPercents(summaryStatuses.map((s) => summary.percentOfExpected[s])))}
            </TableCell>
            <TableCell>
              {ofExecuted(sumPercents(resultStatuses.map((s) => summary.percentOfExecuted[s])))}
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      <p className="text-muted-foreground text-xs">
        Each executed TC-ID counts once with its aggregated status (failed &gt; error &gt; skipped &gt;
        passed); a retried test counts with its last attempt. The expected universe is the snapshot taken when
        the run was created (the active automated test cases, of its suite if it names one; a manual run's own
        selection)
        {summary.amendedTestCaseIds.length > 0 ? ', plus the test cases a maintainer included later' : ''}.
      </p>
      {flaky.length > 0 && (
        <div className="text-sm" data-testid="flaky-cases">
          <span className="font-medium">Flaky (passed on a retry): </span>
          {flaky.map((c, i) => (
            <span key={c.testCaseId}>
              {i > 0 && ', '}
              <Link to={`/test-cases/${c.testCaseId}`} className="font-mono underline">
                {c.testCaseKey}
              </Link>
            </span>
          ))}
        </div>
      )}
      {untested.length > 0 && (
        <div className="text-sm">
          <span className="font-medium">Untested: </span>
          {untested.map((c, i) => (
            <span key={c.testCaseId}>
              {i > 0 && ', '}
              <Link to={`/test-cases/${c.testCaseId}`} className="font-mono underline">
                {c.testCaseKey}
              </Link>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

import { appendFileSync } from 'node:fs'
import path from 'node:path'

import type { FullConfig, FullResult, Reporter, Suite, TestCase, TestResult } from '@playwright/test/reporter'

type Outcome = 'passed' | 'failed' | 'flaky' | 'skipped'

interface Row {
  file: string
  title: string
  outcome: Outcome
  duration: number
  error?: string
}

const icon: Record<Outcome, string> = { passed: '✅', failed: '❌', flaky: '⚠️', skipped: '⏭️' }

/**
 * Writes a visual summary of the journeys to the GitHub Actions job summary (GITHUB_STEP_SUMMARY): totals, a
 * pass/fail bar, one row per spec, every failure with its error and the full list. Outside Actions it does nothing.
 */
export default class SummaryReporter implements Reporter {
  private readonly rows = new Map<string, Row>()
  private rootDir = ''

  onBegin(config: FullConfig, _suite: Suite) {
    this.rootDir = config.rootDir
  }

  onTestEnd(test: TestCase, result: TestResult) {
    const outcome = test.outcome()
    this.rows.set(test.id, {
      file: path.relative(this.rootDir, test.location.file),
      title: test.title,
      outcome: outcome === 'expected' ? 'passed' : outcome === 'unexpected' ? 'failed' : outcome,
      duration: (this.rows.get(test.id)?.duration ?? 0) + result.duration,
      error: result.errors.map((e) => e.message ?? e.value ?? '').join('\n') || undefined,
    })
  }

  onEnd(result: FullResult) {
    const out = process.env.GITHUB_STEP_SUMMARY
    if (!out) return
    appendFileSync(out, render([...this.rows.values()], result, process.env.E2E_REMOTE_URL))
  }
}

export function render(rows: Row[], result: Pick<FullResult, 'status' | 'duration'>, target?: string): string {
  const count = (o: Outcome) => rows.filter((r) => r.outcome === o).length
  const totals = { passed: count('passed'), failed: count('failed'), flaky: count('flaky'), skipped: count('skipped') }
  const ran = rows.length - totals.skipped
  const lines: string[] = []
  lines.push(`## ${result.status === 'passed' ? '✅' : '❌'} E2E journeys · ${target ? `\`${target}\`` : 'local instrumented stack'}`, '')
  lines.push(
    `**${totals.passed} passed** · **${totals.failed} failed** · ${totals.flaky} flaky · ${totals.skipped} skipped · ` +
      `${ran ? Math.round(((totals.passed + totals.flaky) / ran) * 100) : 0}% of executed passed · ${duration(result.duration)}`,
    '',
  )
  lines.push(bar(totals), '')

  lines.push('| Spec | ✅ | ❌ | ⚠️ | ⏭️ | Time |', '|---|--:|--:|--:|--:|--:|')
  for (const [file, group] of groupBy(rows, (r) => r.file)) {
    const n = (o: Outcome) => group.filter((r) => r.outcome === o).length || ''
    const failing = group.some((r) => r.outcome === 'failed')
    lines.push(
      `| ${failing ? '❌' : '✅'} \`${file}\` | ${n('passed')} | ${n('failed')} | ${n('flaky')} | ${n('skipped')} | ${duration(sum(group))} |`,
    )
  }
  lines.push('')

  const failures = rows.filter((r) => r.outcome === 'failed' || r.outcome === 'flaky')
  if (failures.length) {
    lines.push('### Failures', '')
    for (const r of failures) {
      lines.push(`<details><summary>${icon[r.outcome]} ${escape(r.title)} <sub>${escape(r.file)}</sub></summary>`, '')
      lines.push('```', clip(stripAnsi(r.error ?? 'no error message')), '```', '</details>', '')
    }
  }

  lines.push('<details><summary>Every journey</summary>', '', '| | Journey | Spec | Time |', '|---|---|---|--:|')
  for (const r of rows) lines.push(`| ${icon[r.outcome]} | ${escape(r.title)} | \`${r.file}\` | ${duration(r.duration)} |`)
  lines.push('', '</details>', '')

  const { GITHUB_SERVER_URL: server, GITHUB_REPOSITORY: repo, GITHUB_RUN_ID: run } = process.env
  if (server && repo && run) {
    lines.push(`Full Playwright HTML report (screenshots and traces of failures): [run artifacts](${server}/${repo}/actions/runs/${run}#artifacts).`, '')
  }
  return lines.join('\n')
}

/** A 30-cell bar: green passed, yellow flaky, red failed, white skipped. */
function bar(t: Record<Outcome, number>): string {
  const total = t.passed + t.failed + t.flaky + t.skipped
  if (!total) return ''
  const cells = (n: number) => (n ? Math.max(1, Math.round((n / total) * 30)) : 0)
  return '🟩'.repeat(cells(t.passed)) + '🟨'.repeat(cells(t.flaky)) + '🟥'.repeat(cells(t.failed)) + '⬜'.repeat(cells(t.skipped))
}

function groupBy<T>(items: T[], key: (t: T) => string): Map<string, T[]> {
  const groups = new Map<string, T[]>()
  for (const item of items) groups.set(key(item), [...(groups.get(key(item)) ?? []), item])
  return groups
}

const sum = (rows: Row[]) => rows.reduce((total, r) => total + r.duration, 0)

function duration(ms: number): string {
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
}

const stripAnsi = (text: string) => text.replace(/\u001b\[[0-9;]*m/g, '')

const clip = (text: string) => (text.length > 2000 ? `${text.slice(0, 2000)}\n…` : text).replace(/```/g, "'''")

const escape = (text: string) => text.replace(/[|<>]/g, (c) => ({ '|': '\\|', '<': '&lt;', '>': '&gt;' })[c]!)

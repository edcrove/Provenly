import type { Project, TestCase, TestResult, TestRun, TestRunSummary, TestStep } from '@/api/client'

const at = '2026-09-28T10:00:00Z'

export function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    key: 'TC',
    name: 'Default',
    description: 'Test cases created before projects existed.',
    createdAt: at,
    updatedAt: at,
    ...overrides,
  }
}

export function testCase(overrides: Partial<TestCase> = {}): TestCase {
  const id = overrides.id ?? 153
  const projectKey = overrides.projectKey ?? 'TC'
  const number = overrides.number ?? id
  return {
    id,
    key: `${projectKey}-${number}`,
    projectId: 1,
    projectKey,
    number,
    title: 'Login works',
    description: 'User logs in with valid credentials',
    expectedResult: 'Dashboard is shown',
    status: 'active',
    automated: true,
    createdAt: at,
    updatedAt: at,
    deprecatedAt: null,
    ...overrides,
  }
}

export function testStep(overrides: Partial<TestStep> = {}): TestStep {
  return {
    id: 1,
    testCaseId: 153,
    position: 1,
    action: 'Open the login page',
    expectedResult: 'Form is visible',
    createdAt: at,
    updatedAt: at,
    ...overrides,
  }
}

export function testRun(overrides: Partial<TestRun> = {}): TestRun {
  return {
    id: 7,
    projectId: 1,
    externalRunId: 'github:9876:1',
    provider: 'github',
    providerRunId: '9876',
    runAttempt: 1,
    pipeline: 'ci',
    branch: 'main',
    commit: '0123456789abcdef',
    executionStatus: 'completed',
    outcome: {
      verdict: 'failed',
      executed: 1,
      passed: 0,
      failed: 1,
      error: 0,
      skipped: 0,
      untested: 1,
      passRate: 0,
    },
    expectedCount: 2,
    resultCount: 3,
    createdAt: at,
    startedAt: at,
    completedAt: at,
    ...overrides,
  }
}

export function testResult(overrides: Partial<TestResult> = {}): TestResult {
  const result: TestResult = {
    id: 1,
    testRunId: 7,
    testCaseId: 153,
    testCaseKey: 'TC-153',
    requestedTestCaseId: '153',
    correlation: 'valid',
    testName: 'login chrome',
    className: 'auth',
    suiteName: 'e2e',
    status: 'passed',
    durationMs: 1200,
    errorMessage: '',
    errorDetails: '',
    createdAt: at,
    ...overrides,
  }
  // The key follows the linked test case unless a test sets it.
  if (!('testCaseKey' in overrides))
    result.testCaseKey = result.testCaseId === null ? null : `TC-${result.testCaseId}`
  return result
}

export function summary(overrides: Partial<TestRunSummary> = {}): TestRunSummary {
  return {
    testRunId: 7,
    expectedTotal: 2,
    executedTotal: 1,
    counts: { untested: 1, passed: 0, failed: 1, error: 0, skipped: 0 },
    percentOfExpected: { untested: 50, passed: 0, failed: 50, error: 0, skipped: 0 },
    percentOfExecuted: { passed: 0, failed: 100, error: 0, skipped: 0 },
    executionPercent: 50,
    diagnostics: { missing: 1, malformed: 0, unknown: 0, deprecated: 0, wrongProject: 0, total: 1 },
    outsideUniverse: 0,
    outsideUniverseTestCaseIds: [],
    testCases: [
      { testCaseId: 153, testCaseKey: 'TC-153', status: 'failed', resultCount: 2 },
      { testCaseId: 154, testCaseKey: 'TC-154', status: 'untested', resultCount: 0 },
    ],
    ...overrides,
  }
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { unwrap } from '@/lib/problem'
import type { Correlation, ResultStatus } from '@/lib/status'

import { api, type CreateTestCaseRequest, type UpdateTestCaseRequest } from './client'

const MAX_PAGE = 100

export const keys = {
  testCases: ['test-cases'] as const,
  testCase: (id: number) => ['test-cases', id] as const,
  steps: (id: number) => ['test-cases', id, 'steps'] as const,
  history: (id: number) => ['test-cases', id, 'results'] as const,
  testRuns: ['test-runs'] as const,
  testRun: (id: number) => ['test-runs', id] as const,
}

export function useTestCases(page: number, status?: 'active' | 'deprecated') {
  return useQuery({
    queryKey: [...keys.testCases, 'list', page, status],
    queryFn: async () => unwrap(await api.GET('/api/v1/test-cases', { params: { query: { page, status } } })),
  })
}

export function useTestCase(id: number) {
  return useQuery({
    queryKey: keys.testCase(id),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: id } } })),
  })
}

export function useCreateTestCase() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: CreateTestCaseRequest) => unwrap(await api.POST('/api/v1/test-cases', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useUpdateTestCase(id: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: UpdateTestCaseRequest) =>
      unwrap(
        await api.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: id } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useDeprecateTestCase(id: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: { testCaseId: id } } }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useTestSteps(id: number) {
  return useQuery({
    queryKey: keys.steps(id),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-cases/{testCaseId}/steps', {
          params: { path: { testCaseId: id }, query: { pageSize: MAX_PAGE } },
        }),
      ),
  })
}

export function useStepMutations(id: number) {
  const qc = useQueryClient()
  const onSuccess = () => qc.invalidateQueries({ queryKey: keys.steps(id) })
  const path = { testCaseId: id }
  return {
    create: useMutation({
      mutationFn: async (body: { action: string; expectedResult: string }) =>
        unwrap(await api.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path }, body })),
      onSuccess,
    }),
    update: useMutation({
      mutationFn: async ({ stepId, ...body }: { stepId: number; action: string; expectedResult: string }) =>
        unwrap(
          await api.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
            body,
          }),
        ),
      onSuccess,
    }),
    remove: useMutation({
      mutationFn: async (stepId: number) =>
        unwrap(
          await api.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
          }),
        ),
      onSuccess,
    }),
    reorder: useMutation({
      mutationFn: async (stepIds: number[]) =>
        unwrap(
          await api.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
            params: { path },
            body: { stepIds },
          }),
        ),
      onSuccess,
    }),
  }
}

export function useTestCaseHistory(id: number, page: number) {
  return useQuery({
    queryKey: [...keys.history(id), page],
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-cases/{testCaseId}/results', {
          params: { path: { testCaseId: id }, query: { page } },
        }),
      ),
  })
}

export function useTestRuns(page: number) {
  return useQuery({
    queryKey: [...keys.testRuns, 'list', page],
    queryFn: async () => unwrap(await api.GET('/api/v1/test-runs', { params: { query: { page } } })),
  })
}

export function useTestRun(id: number) {
  return useQuery({
    queryKey: keys.testRun(id),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs/{testRunId}', { params: { path: { testRunId: id } } })),
  })
}

export function useTestRunSummary(id: number) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'summary'],
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: { testRunId: id } } })),
  })
}

export function useTestRunResults(
  id: number,
  page: number,
  status?: ResultStatus,
  correlation?: Correlation,
) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'results', page, status, correlation],
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-runs/{testRunId}/results', {
          params: { path: { testRunId: id }, query: { page, status, correlation } },
        }),
      ),
  })
}

export function useTestRunParseErrors(id: number, page: number) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'parse-errors', page],
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-runs/{testRunId}/parse-errors', {
          params: { path: { testRunId: id }, query: { page } },
        }),
      ),
  })
}

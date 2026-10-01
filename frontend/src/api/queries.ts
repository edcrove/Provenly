import {
  useMutation,
  useQuery,
  useQueryClient,
  type MutateOptions,
  type UseMutationOptions,
} from '@tanstack/react-query'
import { useRef } from 'react'

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

/**
 * useMutation whose mutate ignores calls while the previous one is in flight: a double click fires two events
 * before React re-renders the button as disabled, and must not send the request twice (e.g. two test cases).
 */
function useExclusiveMutation<TData, TVariables = void>(
  options: UseMutationOptions<TData, Error, TVariables>,
) {
  const mutation = useMutation(options)
  const busy = useRef(false)
  const mutate = (...[variables, callbacks]: Parameters<typeof mutation.mutate>) => {
    if (busy.current) return
    busy.current = true
    const options: MutateOptions<TData, Error, TVariables> = {
      ...callbacks,
      onSettled: (...args) => {
        busy.current = false
        callbacks?.onSettled?.(...args)
      },
    }
    mutation.mutate(variables as TVariables, options)
  }
  return { ...mutation, mutate }
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
    enabled: id > 0,
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: id } } })),
  })
}

export function useCreateTestCase() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: CreateTestCaseRequest) => unwrap(await api.POST('/api/v1/test-cases', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useUpdateTestCase(id: number) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: UpdateTestCaseRequest) =>
      unwrap(
        await api.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: id } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useDeprecateTestCase(id: number) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: { testCaseId: id } } }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useReactivateTestCase(id: number) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/api/v1/test-cases/{testCaseId}/reactivate', {
          params: { path: { testCaseId: id } },
        }),
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
    create: useExclusiveMutation({
      mutationFn: async (body: { action: string; expectedResult: string }) =>
        unwrap(await api.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path }, body })),
      onSuccess,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ stepId, ...body }: { stepId: number; action: string; expectedResult: string }) =>
        unwrap(
          await api.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
            body,
          }),
        ),
      onSuccess,
    }),
    remove: useExclusiveMutation({
      mutationFn: async (stepId: number) =>
        unwrap(
          await api.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
          }),
        ),
      onSuccess,
    }),
    reorder: useExclusiveMutation({
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
    enabled: id > 0,
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs/{testRunId}', { params: { path: { testRunId: id } } })),
  })
}

export function useTestRunSummary(id: number) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'summary'],
    enabled: id > 0,
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

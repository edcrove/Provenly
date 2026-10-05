import {
  useMutation,
  useQuery,
  useQueryClient,
  type MutateOptions,
  type UseMutationOptions,
} from '@tanstack/react-query'
import { useRef } from 'react'

import { previousPage } from '@/lib/paging'
import { unwrap } from '@/lib/problem'
import type { MemberRole } from '@/lib/roles'
import type { Correlation, ResultStatus } from '@/lib/status'

import {
  api,
  type AcceptInvitationRequest,
  type CreateInvitationRequest,
  type CreateProjectRequest,
  type CreateTestCaseRequest,
  type UpdateProjectRequest,
  type UpdateTestCaseRequest,
} from './client'

const MAX_PAGE = 100

export const keys = {
  me: ['auth', 'me'] as const,
  users: ['users'] as const,
  invitations: ['invitations'] as const,
  projects: ['projects'] as const,
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

/** The signed-in user; a 401 means nobody is signed in. */
export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: async () => unwrap(await api.GET('/api/v1/auth/me')),
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: { username: string; password: string }) =>
      unwrap(await api.POST('/api/v1/auth/login', { body })),
    // Another user's cached data must never show: start from an empty cache.
    onSuccess: (session) => {
      qc.clear()
      qc.setQueryData(keys.me, session.user)
    },
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async () => unwrap(await api.POST('/api/v1/auth/logout')),
    onSettled: () => {
      qc.clear()
      void qc.invalidateQueries({ queryKey: keys.me })
    },
  })
}

export function useChangePassword() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: { currentPassword: string; newPassword: string }) =>
      unwrap(await api.POST('/api/v1/auth/password', { body })),
    onSuccess: (session) => qc.setQueryData(keys.me, session.user),
  })
}

export function useAcceptInvitation() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: AcceptInvitationRequest) =>
      unwrap(await api.POST('/api/v1/invitations/accept', { body })),
    onSuccess: (session) => {
      qc.clear()
      qc.setQueryData(keys.me, session.user)
    },
  })
}

export function useUsers(page: number, enabled = true) {
  return useQuery({
    queryKey: [...keys.users, page],
    enabled,
    placeholderData: (prev, q) => previousPage([...keys.users, page], prev, q?.queryKey),
    queryFn: async () => unwrap(await api.GET('/api/v1/users', { params: { query: { page } } })),
  })
}

export function useInvitations(page: number, enabled = true) {
  return useQuery({
    queryKey: [...keys.invitations, page],
    enabled,
    placeholderData: (prev, q) => previousPage([...keys.invitations, page], prev, q?.queryKey),
    queryFn: async () => unwrap(await api.GET('/api/v1/invitations', { params: { query: { page } } })),
  })
}

export function useCreateInvitation() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: CreateInvitationRequest) =>
      unwrap(await api.POST('/api/v1/invitations', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.invitations }),
  })
}

export function useRevokeInvitation() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (invitationId: number) =>
      unwrap(
        await api.POST('/api/v1/invitations/{invitationId}/revoke', { params: { path: { invitationId } } }),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.invitations }),
  })
}

export function useProjects(page = 1, pageSize = MAX_PAGE) {
  return useQuery({
    queryKey: [...keys.projects, 'list', pageSize, page],
    placeholderData: (prev, q) => previousPage([...keys.projects, 'list', pageSize, page], prev, q?.queryKey),
    queryFn: async () => unwrap(await api.GET('/api/v1/projects', { params: { query: { page, pageSize } } })),
  })
}

export function useProjectMembers(projectKey: string, page: number) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'members', page],
    placeholderData: (prev, q) =>
      previousPage([...keys.projects, projectKey, 'members', page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/members', {
          params: { path: { projectKey }, query: { page } },
        }),
      ),
  })
}

export function useMemberMutations(projectKey: string) {
  const qc = useQueryClient()
  // Refetch after failures too: the list on screen may be stale.
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'members'] })
  return {
    set: useExclusiveMutation({
      mutationFn: async ({ username, role }: { username: string; role: MemberRole }) =>
        unwrap(
          await api.PUT('/api/v1/projects/{projectKey}/members/{username}', {
            params: { path: { projectKey, username } },
            body: { role },
          }),
        ),
      onSettled,
    }),
    remove: useExclusiveMutation({
      mutationFn: async (username: string) =>
        unwrap(
          await api.DELETE('/api/v1/projects/{projectKey}/members/{username}', {
            params: { path: { projectKey, username } },
          }),
        ),
      onSettled,
    }),
  }
}

export function useApiKeys(projectKey: string, page: number, enabled = true) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'api-keys', page],
    enabled,
    placeholderData: (prev, q) =>
      previousPage([...keys.projects, projectKey, 'api-keys', page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/api-keys', {
          params: { path: { projectKey }, query: { page } },
        }),
      ),
  })
}

export function useApiKeyMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'api-keys'] })
  return {
    create: useExclusiveMutation({
      mutationFn: async (name: string) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/api-keys', {
            params: { path: { projectKey } },
            body: { name },
          }),
        ),
      onSettled,
    }),
    revoke: useExclusiveMutation({
      mutationFn: async (apiKeyId: number) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', {
            params: { path: { projectKey, apiKeyId } },
          }),
        ),
      onSettled,
    }),
  }
}

export function useCreateProject() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: CreateProjectRequest) => unwrap(await api.POST('/api/v1/projects', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.projects }),
  })
}

export function useUpdateProject(key: string) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: UpdateProjectRequest) =>
      unwrap(
        await api.PATCH('/api/v1/projects/{projectKey}', { params: { path: { projectKey: key } }, body }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.projects }),
  })
}

export function useTestCases(page: number, status?: 'active' | 'deprecated', project?: string) {
  return useQuery({
    queryKey: [...keys.testCases, 'list', project, status, page],
    placeholderData: (prev, q) =>
      previousPage([...keys.testCases, 'list', project, status, page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-cases', { params: { query: { page, status, project } } })),
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
  // Refetch after failures too: a 400/404 usually means the list on screen is stale (changed elsewhere).
  const onSettled = () => qc.invalidateQueries({ queryKey: keys.steps(id) })
  const path = { testCaseId: id }
  return {
    create: useExclusiveMutation({
      mutationFn: async (body: { action: string; expectedResult: string }) =>
        unwrap(await api.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path }, body })),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ stepId, ...body }: { stepId: number; action: string; expectedResult: string }) =>
        unwrap(
          await api.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
            body,
          }),
        ),
      onSettled,
    }),
    remove: useExclusiveMutation({
      mutationFn: async (stepId: number) =>
        unwrap(
          await api.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
          }),
        ),
      onSettled,
    }),
    reorder: useExclusiveMutation({
      mutationFn: async (stepIds: number[]) =>
        unwrap(
          await api.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
            params: { path },
            body: { stepIds },
          }),
        ),
      onSettled,
    }),
  }
}

export function useTestCaseHistory(id: number, page: number) {
  return useQuery({
    queryKey: [...keys.history(id), page],
    placeholderData: (prev, q) => previousPage([...keys.history(id), page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-cases/{testCaseId}/results', {
          params: { path: { testCaseId: id }, query: { page } },
        }),
      ),
  })
}

export function useTestRuns(page: number, project?: string) {
  return useQuery({
    queryKey: [...keys.testRuns, 'list', project, page],
    placeholderData: (prev, q) => previousPage([...keys.testRuns, 'list', project, page], prev, q?.queryKey),
    queryFn: async () => unwrap(await api.GET('/api/v1/test-runs', { params: { query: { page, project } } })),
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
    queryKey: [...keys.testRun(id), 'results', status, correlation, page],
    placeholderData: (prev, q) =>
      previousPage([...keys.testRun(id), 'results', status, correlation, page], prev, q?.queryKey),
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
    placeholderData: (prev, q) =>
      previousPage([...keys.testRun(id), 'parse-errors', page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-runs/{testRunId}/parse-errors', {
          params: { path: { testRunId: id }, query: { page } },
        }),
      ),
  })
}

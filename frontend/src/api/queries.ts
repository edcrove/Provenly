import {
  useMutation,
  useQuery,
  useQueryClient,
  type MutateOptions,
  type UseMutationOptions,
} from '@tanstack/react-query'
import { useRef } from 'react'

import { previousPage } from '@/lib/paging'
import { ApiError, unwrap } from '@/lib/problem'
import type { MemberRole } from '@/lib/roles'
import type { Correlation, ResultStatus } from '@/lib/status'

import {
  api,
  type TestCase,
  type AcceptInvitationRequest,
  type CreateInvitationRequest,
  type CreateProjectRequest,
  type CreateIssueRequest,
  type CreateRequirementRequest,
  type CreateSuiteRequest,
  type UpdateIssueRequest,
  type UpdateRequirementRequest,
  type ManualResultRequest,
  type StartManualRunRequest,
  type CreateTestCaseRequest,
  type UpdateSuiteRequest,
  type UpdateDimensionRequest,
  type UpdateProjectRequest,
  type UpdateTestCaseRequest,
  type UpdateWebhookRequest,
  type ConnectGitHubRequest,
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

/** Sets a new password with a reset link and signs in (public). */
export function useResetPassword() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: { token: string; password: string }) =>
      unwrap(await api.POST('/api/v1/password-reset', { body })),
    onSuccess: (session) => {
      qc.clear()
      qc.setQueryData(keys.me, session.user)
    },
  })
}

/** Administrators: deactivate or reactivate a user (their sessions stop at once / they can sign in again). */
export function useUserActivation() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async ({ username, active }: { username: string; active: boolean }) =>
      unwrap(
        await api.POST(
          active ? '/api/v1/users/{username}/reactivate' : '/api/v1/users/{username}/deactivate',
          {
            params: { path: { username } },
          },
        ),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.users }),
  })
}

/** Administrators: a single-use password reset link for a user (its token is shown once). */
export function useCreatePasswordReset() {
  return useExclusiveMutation({
    mutationFn: async (username: string) =>
      unwrap(await api.POST('/api/v1/users/{username}/password-reset', { params: { path: { username } } })),
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

/** Narrows the test case list: a tag and `dimension:value` pairs that must all hold. */
export interface TestCaseFilter {
  status?: 'active' | 'deprecated'
  project?: string
  tag?: string
  classification?: string
  /** A suite of `project`. */
  suite?: string
  /** The test case with this key (`CHK-12`): zero or one item. */
  key?: string
  pageSize?: number
}

export function useTestCases(page: number, filter: TestCaseFilter = {}) {
  const { status, project, tag, classification, suite, key: tcKey, pageSize } = filter
  const key = [...keys.testCases, 'list', project, status, tag, classification, suite, tcKey, pageSize, page]
  return useQuery({
    queryKey: key,
    placeholderData: (prev, q) => previousPage(key, prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-cases', {
          params: { query: { page, pageSize, status, project, tag, classification, suite, key: tcKey } },
        }),
      ),
  })
}

export function useSuites(projectKey: string) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'suites'],
    enabled: projectKey !== '',
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/projects/{projectKey}/suites', { params: { path: { projectKey } } })),
  })
}

export function useSuite(projectKey: string, suiteKey: string) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'suites', suiteKey],
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
          params: { path: { projectKey, suiteKey } },
        }),
      ),
  })
}

export function useSuiteMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = async () => {
    await qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'suites'] })
    await qc.invalidateQueries({ queryKey: keys.testCases })
  }
  return {
    create: useExclusiveMutation({
      mutationFn: async (body: CreateSuiteRequest) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/suites', { params: { path: { projectKey } }, body }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ suiteKey, ...body }: UpdateSuiteRequest & { suiteKey: string }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
            params: { path: { projectKey, suiteKey } },
            body,
          }),
        ),
      onSettled,
    }),
    setCases: useExclusiveMutation({
      mutationFn: async ({ suiteKey, testCaseIds }: { suiteKey: string; testCaseIds: number[] }) =>
        unwrap(
          await api.PUT('/api/v1/projects/{projectKey}/suites/{suiteKey}/cases', {
            params: { path: { projectKey, suiteKey } },
            body: { testCaseIds },
          }),
        ),
      onSettled,
    }),
  }
}

export function useRequirements(projectKey: string, testCase?: number) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'requirements', testCase],
    enabled: projectKey !== '',
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/requirements', {
          params: { path: { projectKey }, query: { testCase } },
        }),
      ),
  })
}

export function useRequirement(projectKey: string, requirementId: number) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'requirements', 'one', requirementId],
    enabled: requirementId > 0,
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
          params: { path: { projectKey, requirementId } },
        }),
      ),
  })
}

export function useRequirementMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'requirements'] })
  return {
    create: useExclusiveMutation({
      mutationFn: async (body: CreateRequirementRequest) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/requirements', {
            params: { path: { projectKey } },
            body,
          }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ requirementId, ...body }: UpdateRequirementRequest & { requirementId: number }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
            params: { path: { projectKey, requirementId } },
            body,
          }),
        ),
      onSettled,
    }),
    link: useExclusiveMutation({
      mutationFn: async ({ requirementId, testCaseIds }: { requirementId: number; testCaseIds: number[] }) =>
        unwrap(
          await api.PUT('/api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases', {
            params: { path: { projectKey, requirementId } },
            body: { testCaseIds },
          }),
        ),
      onSettled,
    }),
  }
}

export function useIssues(projectKey: string, filter: { testCase?: number; state?: 'open' | 'closed' } = {}) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'issues', filter.testCase, filter.state],
    enabled: projectKey !== '',
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/issues', {
          params: { path: { projectKey }, query: filter },
        }),
      ),
  })
}

export function useIssue(projectKey: string, issueId: number) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'issues', 'one', issueId],
    enabled: issueId > 0,
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/issues/{issueId}', {
          params: { path: { projectKey, issueId } },
        }),
      ),
  })
}

export function useIssueMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'issues'] })
  return {
    create: useExclusiveMutation({
      mutationFn: async (body: CreateIssueRequest) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/issues', {
            params: { path: { projectKey } },
            body,
          }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ issueId, ...body }: UpdateIssueRequest & { issueId: number }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/issues/{issueId}', {
            params: { path: { projectKey, issueId } },
            body,
          }),
        ),
      onSettled,
    }),
    link: useExclusiveMutation({
      mutationFn: async ({ issueId, testCaseIds }: { issueId: number; testCaseIds: number[] }) =>
        unwrap(
          await api.PUT('/api/v1/projects/{projectKey}/issues/{issueId}/test-cases', {
            params: { path: { projectKey, issueId } },
            body: { testCaseIds },
          }),
        ),
      onSettled,
    }),
  }
}

export function useQuality(projectKey: string, staleDays: number, window: number) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'quality', staleDays, window],
    enabled: projectKey !== '',
    // Changing the stale days or the flaky window keeps the current figures on screen (and the selects mounted,
    // with their focus) until the new ones arrive; another project starts from loading.
    placeholderData: (prev, q) => (q?.queryKey[keys.projects.length] === projectKey ? prev : undefined),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/quality', {
          params: { path: { projectKey }, query: { staleDays, window } },
        }),
      ),
  })
}

export function useDimensions(projectKey: string, enabled = true) {
  return useQuery({
    queryKey: [...keys.projects, projectKey, 'dimensions'],
    enabled: enabled && projectKey !== '',
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: { projectKey } } })),
  })
}

export function useDimensionMutations(projectKey: string) {
  const qc = useQueryClient()
  // Refetch after failures too: someone else may have added the same key.
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey, 'dimensions'] })
  return {
    create: useExclusiveMutation({
      mutationFn: async (body: { key: string; name: string }) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/dimensions', {
            params: { path: { projectKey } },
            body,
          }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ dimensionKey, ...body }: UpdateDimensionRequest & { dimensionKey: string }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}', {
            params: { path: { projectKey, dimensionKey } },
            body,
          }),
        ),
      onSettled,
    }),
    createValue: useExclusiveMutation({
      mutationFn: async ({ dimensionKey, ...body }: { dimensionKey: string; key: string; name: string }) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
            params: { path: { projectKey, dimensionKey } },
            body,
          }),
        ),
      onSettled,
    }),
    updateValue: useExclusiveMutation({
      mutationFn: async ({
        dimensionKey,
        valueKey,
        ...body
      }: UpdateDimensionRequest & { dimensionKey: string; valueKey: string }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}', {
            params: { path: { projectKey, dimensionKey, valueKey } },
            body,
          }),
        ),
      onSettled,
    }),
  }
}

export function useTestCase(id: number) {
  return useQuery({
    queryKey: keys.testCase(id),
    enabled: id > 0,
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: id } } })),
  })
}

/** If-Match with the version of the test case on screen (optimistic locking, MVP D7). */
function ifMatch(qc: ReturnType<typeof useQueryClient>, id: number) {
  const version = qc.getQueryData<TestCase>(keys.testCase(id))?.version
  return version ? { 'If-Match': `"${version}"` } : undefined
}

/** Keeps the version on screen in step with a step write (its ETag), so the next write is not refused. */
function keepVersion(qc: ReturnType<typeof useQueryClient>, id: number, response: Response) {
  const version = Number(response.headers.get('ETag')?.replaceAll('"', ''))
  if (response.ok && Number.isInteger(version) && version > 0)
    qc.setQueryData<TestCase>(keys.testCase(id), (tc) => (tc ? { ...tc, version } : tc))
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
        await api.PATCH('/api/v1/test-cases/{testCaseId}', {
          params: { path: { testCaseId: id } },
          body,
          headers: ifMatch(qc, id),
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testCases }),
  })
}

export function useDeprecateTestCase(id: number) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST('/api/v1/test-cases/{testCaseId}/deprecate', {
          params: { path: { testCaseId: id } },
          headers: ifMatch(qc, id),
        }),
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
          headers: ifMatch(qc, id),
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
  // Refetch after failures too: a 400/404/412 usually means the list on screen is stale (changed elsewhere).
  const onSettled = () => qc.invalidateQueries({ queryKey: keys.steps(id) })
  const path = { testCaseId: id }
  /** Sends the write with If-Match and keeps the new version (ETag) for the next one. */
  const send = async <T>(
    call: (headers?: Record<string, string>) => Promise<{ data?: T; error?: unknown; response: Response }>,
  ) => {
    const result = await call(ifMatch(qc, id))
    keepVersion(qc, id, result.response)
    return unwrap(result)
  }
  return {
    create: useExclusiveMutation({
      mutationFn: (body: { action: string; expectedResult: string }) =>
        send((headers) =>
          api.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path }, body, headers }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: ({ stepId, ...body }: { stepId: number; action: string; expectedResult: string }) =>
        send((headers) =>
          api.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
            body,
            headers,
          }),
        ),
      onSettled,
    }),
    remove: useExclusiveMutation({
      mutationFn: (stepId: number) =>
        send((headers) =>
          api.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
            params: { path: { ...path, stepId } },
            headers,
          }),
        ),
      onSettled,
    }),
    reorder: useExclusiveMutation({
      mutationFn: (stepIds: number[]) =>
        send((headers) =>
          api.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
            params: { path },
            body: { stepIds },
            headers,
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

export function useTestRuns(page: number, project?: string, suite?: string) {
  const key = [...keys.testRuns, 'list', project, suite, page]
  return useQuery({
    queryKey: key,
    placeholderData: (prev, q) => previousPage(key, prev, q?.queryKey),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs', { params: { query: { page, project, suite } } })),
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

export function useTestRunSummary(id: number, running = false) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'summary'],
    enabled: id > 0,
    refetchInterval: running ? 2000 : false,
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: { testRunId: id } } })),
  })
}

/** The live state of a live run, polled every two seconds while it runs (provisional; the final report decides). */
export function useLiveRun(id: number, running: boolean) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'live'],
    enabled: id > 0,
    refetchInterval: running ? 2000 : false,
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: { testRunId: id } } })),
  })
}

export function useRunAmendments(id: number, enabled = true) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'amendments'],
    enabled: enabled && id > 0,
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-runs/{testRunId}/amendments', {
          params: { path: { testRunId: id }, query: { pageSize: MAX_PAGE } },
        }),
      ),
  })
}

/** Includes a reported TC-ID in a run's universe (DEC-42); the run, its summary and the run list change. */
export function useAmendRun(id: number) {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: { testCaseId: number; reason: string }) =>
      unwrap(
        await api.POST('/api/v1/test-runs/{testRunId}/amendments', {
          params: { path: { testRunId: id } },
          body,
        }),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.testRuns }),
  })
}

export function useStartManualRun() {
  const qc = useQueryClient()
  return useExclusiveMutation({
    mutationFn: async (body: StartManualRunRequest) =>
      unwrap(await api.POST('/api/v1/test-runs/manual', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.testRuns }),
  })
}

/** Record and finish actions of a running manual run; every write refreshes the run, its summary and results. */
export function useManualRun(id: number) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: keys.testRuns })
  return {
    record: useExclusiveMutation({
      mutationFn: async (body: ManualResultRequest) =>
        unwrap(
          await api.POST('/api/v1/test-runs/{testRunId}/manual-results', {
            params: { path: { testRunId: id } },
            body,
          }),
        ),
      onSettled,
    }),
    finish: useExclusiveMutation({
      mutationFn: async (status: 'completed' | 'cancelled') =>
        unwrap(
          await api.POST('/api/v1/test-runs/{testRunId}/finish', {
            params: { path: { testRunId: id } },
            body: { status },
          }),
        ),
      onSettled,
    }),
  }
}

export function useTestRunResults(
  id: number,
  page: number,
  status?: ResultStatus,
  correlation?: Correlation,
  shard?: number,
) {
  return useQuery({
    queryKey: [...keys.testRun(id), 'results', status, correlation, shard, page],
    placeholderData: (prev, q) =>
      previousPage([...keys.testRun(id), 'results', status, correlation, shard, page], prev, q?.queryKey),
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/test-runs/{testRunId}/results', {
          params: { path: { testRunId: id }, query: { page, status, correlation, shard } },
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

const webhooksKey = (projectKey: string) => [...keys.projects, projectKey, 'webhooks'] as const

export function useWebhooks(projectKey: string) {
  return useQuery({
    queryKey: webhooksKey(projectKey),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: { projectKey } } })),
  })
}

export function useWebhookDeliveries(projectKey: string, webhookId: number, page: number, enabled: boolean) {
  return useQuery({
    queryKey: [...webhooksKey(projectKey), webhookId, 'deliveries', page],
    enabled,
    queryFn: async () =>
      unwrap(
        await api.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', {
          params: { path: { projectKey, webhookId }, query: { page } },
        }),
      ),
  })
}

export function useWebhookMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: webhooksKey(projectKey) })
  return {
    create: useExclusiveMutation({
      mutationFn: async (url: string) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/webhooks', {
            params: { path: { projectKey } },
            body: { url, events: ['run.completed'] },
          }),
        ),
      onSettled,
    }),
    update: useExclusiveMutation({
      mutationFn: async ({ webhookId, body }: { webhookId: number; body: UpdateWebhookRequest }) =>
        unwrap(
          await api.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
            params: { path: { projectKey, webhookId } },
            body,
          }),
        ),
      onSettled,
    }),
    ping: useExclusiveMutation({
      mutationFn: async (webhookId: number) =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', {
            params: { path: { projectKey, webhookId } },
          }),
        ),
      onSettled,
    }),
  }
}

const githubKey = (projectKey: string) => [...keys.projects, projectKey, 'github'] as const

/** The project's GitHub connection, or null when it has none. */
export function useGitHubConnection(projectKey: string) {
  return useQuery({
    queryKey: githubKey(projectKey),
    queryFn: async () => {
      try {
        return unwrap(
          await api.GET('/api/v1/projects/{projectKey}/github', { params: { path: { projectKey } } }),
        )
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
}

export function useGitHubMutations(projectKey: string) {
  const qc = useQueryClient()
  const onSettled = () => qc.invalidateQueries({ queryKey: [...keys.projects, projectKey] })
  return {
    connect: useExclusiveMutation({
      mutationFn: async (body: ConnectGitHubRequest) =>
        unwrap(
          await api.PUT('/api/v1/projects/{projectKey}/github', { params: { path: { projectKey } }, body }),
        ),
      onSettled,
    }),
    disconnect: useExclusiveMutation({
      mutationFn: async () =>
        unwrap(
          await api.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: { projectKey } } }),
        ),
      onSettled,
    }),
    sync: useExclusiveMutation({
      mutationFn: async () =>
        unwrap(
          await api.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: { projectKey } } }),
        ),
      onSettled,
    }),
  }
}

export interface AuditFilter {
  project?: string
  actor?: string
}

export function useAuditEvents(filter: AuditFilter, page: number) {
  const key = ['audit', filter.project ?? '', filter.actor ?? '', page]
  return useQuery({
    queryKey: key,
    placeholderData: (prev, q) => previousPage(key, prev, q?.queryKey),
    queryFn: async () => unwrap(await api.GET('/api/v1/audit', { params: { query: { ...filter, page } } })),
  })
}

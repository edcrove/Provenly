import createClient from 'openapi-fetch'

import type { components, paths } from './schema'

export type User = components['schemas']['User']
export type Member = components['schemas']['Member']
export type ApiKey = components['schemas']['ApiKey']
export type Invitation = components['schemas']['Invitation']
export type AcceptInvitationRequest = components['schemas']['AcceptInvitationRequest']
export type CreateInvitationRequest = components['schemas']['CreateInvitationRequest']
export type Project = components['schemas']['Project']
export type CreateProjectRequest = components['schemas']['CreateProjectRequest']
export type UpdateProjectRequest = components['schemas']['UpdateProjectRequest']
export type TestCase = components['schemas']['TestCase']
export type TestStep = components['schemas']['TestStep']
export type TestRun = components['schemas']['TestRun']
export type TestResult = components['schemas']['TestResult']
export type TestRunSummary = components['schemas']['TestRunSummary']
export type TestCaseResult = components['schemas']['TestCaseResult']
export type ParseError = components['schemas']['ParseError']
export type CreateTestCaseRequest = components['schemas']['CreateTestCaseRequest']
export type UpdateTestCaseRequest = components['schemas']['UpdateTestCaseRequest']

/** Typed client generated from api/openapi.yaml (openapi-typescript + openapi-fetch). */
export function createApiClient(baseUrl: string) {
  // Resolve fetch at call time so test interceptors (MSW) patched later still apply.
  return createClient<paths>({ baseUrl, fetch: (request) => globalThis.fetch(request) })
}

export type ApiClient = ReturnType<typeof createApiClient>

export const api = createApiClient(globalThis.location?.origin ?? 'http://localhost')

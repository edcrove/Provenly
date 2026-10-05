import createClient from 'openapi-fetch'

import type { components, paths } from './schema'

export type User = components['schemas']['User']
export type Member = components['schemas']['Member']
export type ApiKey = components['schemas']['ApiKey']
export type AuditEvent = components['schemas']['AuditEvent']
export type Webhook = components['schemas']['Webhook']
export type WebhookDelivery = components['schemas']['WebhookDelivery']
export type UpdateWebhookRequest = components['schemas']['UpdateWebhookRequest']
export type GitHubConnection = components['schemas']['GitHubConnection']
export type ConnectGitHubRequest = components['schemas']['ConnectGitHubRequest']
export type Amendment = components['schemas']['Amendment']
export type Invitation = components['schemas']['Invitation']
export type AcceptInvitationRequest = components['schemas']['AcceptInvitationRequest']
export type CreateInvitationRequest = components['schemas']['CreateInvitationRequest']
export type Project = components['schemas']['Project']
export type CreateProjectRequest = components['schemas']['CreateProjectRequest']
export type UpdateProjectRequest = components['schemas']['UpdateProjectRequest']
export type TestCase = components['schemas']['TestCase']
export type Dimension = components['schemas']['Dimension']
export type Suite = components['schemas']['Suite']
export type Requirement = components['schemas']['Requirement']
export type CreateRequirementRequest = components['schemas']['CreateRequirementRequest']
export type UpdateRequirementRequest = components['schemas']['UpdateRequirementRequest']
export type Issue = components['schemas']['Issue']
export type LiveRun = components['schemas']['LiveRun']
export type CreateIssueRequest = components['schemas']['CreateIssueRequest']
export type UpdateIssueRequest = components['schemas']['UpdateIssueRequest']
export type StartManualRunRequest = components['schemas']['StartManualRunRequest']
export type ManualResultRequest = components['schemas']['ManualResultRequest']
export type CreateSuiteRequest = components['schemas']['CreateSuiteRequest']
export type UpdateSuiteRequest = components['schemas']['UpdateSuiteRequest']
export type DimensionValue = components['schemas']['DimensionValue']
export type UpdateDimensionRequest = components['schemas']['UpdateDimensionRequest']
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

import { Navigate, type RouteObject } from 'react-router'

import { AcceptInvitePage } from '@/features/auth/AcceptInvitePage'
import { AccountPage } from '@/features/auth/AccountPage'
import { LoginPage } from '@/features/auth/LoginPage'
import { UsersPage } from '@/features/auth/UsersPage'
import { DashboardPage } from '@/features/dashboard/DashboardPage'
import { IssueDetailPage } from '@/features/issues/IssueDetailPage'
import { IssuesPage } from '@/features/issues/IssuesPage'
import { ProjectMembersPage } from '@/features/projects/ProjectMembersPage'
import { ProjectsPage } from '@/features/projects/ProjectsPage'
import { RequirementDetailPage } from '@/features/requirements/RequirementDetailPage'
import { RequirementsPage } from '@/features/requirements/RequirementsPage'
import { SuiteDetailPage } from '@/features/suites/SuiteDetailPage'
import { SuitesPage } from '@/features/suites/SuitesPage'
import { NewManualRunPage } from '@/features/manual/NewManualRunPage'
import { NewTestCasePage } from '@/features/test-cases/NewTestCasePage'
import { TestCaseDetailPage } from '@/features/test-cases/TestCaseDetailPage'
import { TestCaseListPage } from '@/features/test-cases/TestCaseListPage'
import { TestRunDetailPage } from '@/features/test-runs/TestRunDetailPage'
import { TestRunListPage } from '@/features/test-runs/TestRunListPage'

import { Layout } from './Layout'
import { NotFoundPage } from './NotFoundPage'

export const routes: RouteObject[] = [
  { path: 'login', element: <LoginPage /> },
  { path: 'accept-invite', element: <AcceptInvitePage /> },
  {
    element: <Layout />,
    children: [
      { index: true, element: <Navigate to="/test-cases" replace /> },
      { path: 'projects', element: <ProjectsPage /> },
      { path: 'projects/:projectKey', element: <ProjectMembersPage /> },
      { path: 'users', element: <UsersPage /> },
      { path: 'account', element: <AccountPage /> },
      { path: 'test-cases', element: <TestCaseListPage /> },
      { path: 'test-cases/new', element: <NewTestCasePage /> },
      { path: 'test-cases/:testCaseId', element: <TestCaseDetailPage /> },
      { path: 'suites', element: <SuitesPage /> },
      { path: 'requirements', element: <RequirementsPage /> },
      { path: 'requirements/:projectKey/:requirementId', element: <RequirementDetailPage /> },
      { path: 'dashboard', element: <DashboardPage /> },
      { path: 'issues', element: <IssuesPage /> },
      { path: 'issues/:projectKey/:issueId', element: <IssueDetailPage /> },
      { path: 'suites/:projectKey/:suiteKey', element: <SuiteDetailPage /> },
      { path: 'test-runs', element: <TestRunListPage /> },
      { path: 'test-runs/manual', element: <NewManualRunPage /> },
      { path: 'test-runs/:testRunId', element: <TestRunDetailPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

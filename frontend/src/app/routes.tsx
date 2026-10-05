import { Navigate, type RouteObject } from 'react-router'

import { AcceptInvitePage } from '@/features/auth/AcceptInvitePage'
import { AccountPage } from '@/features/auth/AccountPage'
import { LoginPage } from '@/features/auth/LoginPage'
import { UsersPage } from '@/features/auth/UsersPage'
import { ProjectMembersPage } from '@/features/projects/ProjectMembersPage'
import { ProjectsPage } from '@/features/projects/ProjectsPage'
import { SuiteDetailPage } from '@/features/suites/SuiteDetailPage'
import { SuitesPage } from '@/features/suites/SuitesPage'
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
      { path: 'suites/:projectKey/:suiteKey', element: <SuiteDetailPage /> },
      { path: 'test-runs', element: <TestRunListPage /> },
      { path: 'test-runs/:testRunId', element: <TestRunDetailPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

import { Navigate, type RouteObject } from 'react-router'

import { NewTestCasePage } from '@/features/test-cases/NewTestCasePage'
import { TestCaseDetailPage } from '@/features/test-cases/TestCaseDetailPage'
import { TestCaseListPage } from '@/features/test-cases/TestCaseListPage'
import { TestRunDetailPage } from '@/features/test-runs/TestRunDetailPage'
import { TestRunListPage } from '@/features/test-runs/TestRunListPage'

import { Layout } from './Layout'
import { NotFoundPage } from './NotFoundPage'

export const routes: RouteObject[] = [
  {
    element: <Layout />,
    children: [
      { index: true, element: <Navigate to="/test-cases" replace /> },
      { path: 'test-cases', element: <TestCaseListPage /> },
      { path: 'test-cases/new', element: <NewTestCasePage /> },
      { path: 'test-cases/:testCaseId', element: <TestCaseDetailPage /> },
      { path: 'test-runs', element: <TestRunListPage /> },
      { path: 'test-runs/:testRunId', element: <TestRunDetailPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

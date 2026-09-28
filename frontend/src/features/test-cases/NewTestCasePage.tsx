import { useNavigate } from 'react-router'

import { useCreateTestCase } from '@/api/queries'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

import { TestCaseForm } from './TestCaseForm'

export function NewTestCasePage() {
  const navigate = useNavigate()
  const create = useCreateTestCase()
  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <CardTitle className="text-xl">New test case</CardTitle>
        <CardDescription>The TC-ID is assigned by Provenly and never changes or gets reused.</CardDescription>
      </CardHeader>
      <CardContent>
        <TestCaseForm
          submitLabel="Create test case"
          pending={create.isPending}
          error={create.error}
          onSubmit={(values) =>
            create.mutate(values, { onSuccess: (tc) => navigate(`/test-cases/${tc.id}`) })
          }
          onCancel={() => navigate('/test-cases')}
        />
      </CardContent>
    </Card>
  )
}

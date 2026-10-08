import { ErrorAlert } from '@/components/QueryState'
import { PageTitle } from '@/components/PageTitle'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

/** A page the signed-in user's role does not allow: says why, instead of a form or request that would fail. */
export function NotAllowed({ title, reason }: { title: string; reason: string }) {
  return (
    <Card>
      <CardHeader>
        <PageTitle title={title} />
        <CardTitle as="h1" className="text-xl">
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ErrorAlert error={new Error(reason)} title="Not allowed" />
      </CardContent>
    </Card>
  )
}

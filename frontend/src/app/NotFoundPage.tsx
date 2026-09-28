import { Link } from 'react-router'

export function NotFoundPage() {
  return (
    <div className="space-y-2">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <Link to="/test-cases" className="text-sm underline">
        Go to test cases
      </Link>
    </div>
  )
}

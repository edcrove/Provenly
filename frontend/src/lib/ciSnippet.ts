/** The CI step that reports a JUnit file with the key (GitHub Actions variables as an example). */
export function ciSnippet(origin: string, projectKey: string) {
  return [
    `curl -fsS -X POST "${origin}/api/v1/ingestion/junit?project=${projectKey}&provider=github&runId=$GITHUB_RUN_ID&runAttempt=$GITHUB_RUN_ATTEMPT&branch=$GITHUB_REF_NAME&commit=$GITHUB_SHA" \\`,
    `  -H "Authorization: Bearer $PROVENLY_API_KEY" -H "Content-Type: application/xml" \\`,
    `  --data-binary @junit.xml`,
  ].join('\n')
}

/**
 * The CI step that reports a JUnit file with the key (GitHub Actions variables as an example), gzip-compressed. The
 * branch is GITHUB_HEAD_REF on pull_request builds (GITHUB_REF_NAME is "123/merge" there) and the run links to its job.
 */
export function ciSnippet(origin: string, projectKey: string) {
  return [
    `gzip -c junit.xml | curl -fsS -X POST "${origin}/api/v1/ingestion/junit?project=${projectKey}&provider=github&runId=$GITHUB_RUN_ID&runAttempt=$GITHUB_RUN_ATTEMPT&branch=\${GITHUB_HEAD_REF:-$GITHUB_REF_NAME}&commit=$GITHUB_SHA&ciUrl=$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/attempts/$GITHUB_RUN_ATTEMPT" \\`,
    `  -H "Authorization: Bearer $PROVENLY_API_KEY" -H "Content-Type: application/xml" -H "Content-Encoding: gzip" \\`,
    `  --data-binary @-`,
  ].join('\n')
}

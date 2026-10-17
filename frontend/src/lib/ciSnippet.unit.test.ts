import { describe, expect, it } from 'vitest'

import { ciSnippet } from './ciSnippet'

describe('ciSnippet', () => {
  it('posts the JUnit file to the project with the key from a CI secret', () => {
    const s = ciSnippet('https://provenly.example', 'CHK')
    expect(s).toContain(
      '"https://provenly.example/api/v1/ingestion/junit?project=CHK&provider=github&runId=$GITHUB_RUN_ID',
    )
    expect(s).toContain('-H "Authorization: Bearer $PROVENLY_API_KEY"')
    expect(s).toContain('gzip -c junit.xml | curl')
    expect(s).toContain('-H "Content-Encoding: gzip"')
    expect(s).toContain('--data-binary @-')
    // Audit 2026-10-17: the branch of a pull_request build and the link to the job.
    expect(s).toContain('&branch=${GITHUB_HEAD_REF:-$GITHUB_REF_NAME}&commit=$GITHUB_SHA')
    expect(s).toContain(
      '&ciUrl=$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/attempts/$GITHUB_RUN_ATTEMPT"',
    )
  })
})

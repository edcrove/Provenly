import { remoteURL } from '../playwright.config'
import { sweepAsAdmin } from './cleanup'
import remapCoverage from './remap-coverage'

/** Playwright global teardown: the journeys leave the instance clean; locally, coverage evidence is remapped too. */
export default async function teardown() {
  await sweepAsAdmin('after the run')
  if (!remoteURL) await remapCoverage()
}

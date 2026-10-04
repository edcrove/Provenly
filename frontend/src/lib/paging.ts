/**
 * Placeholder for a paginated query while its next page loads: the previous data stays on screen only when the
 * previous query differs from the new one by the page alone (the last key element). Any other change (another
 * test case, run or filter) shows the loading state instead of someone else's rows.
 */
export function previousPage<T>(
  key: readonly unknown[],
  previous: T | undefined,
  previousKey: readonly unknown[] | undefined,
): T | undefined {
  if (!previousKey || previousKey.length !== key.length) return undefined
  const samePageless = key.every((k, i) => i === key.length - 1 || Object.is(k, previousKey[i]))
  return samePageless ? previous : undefined
}

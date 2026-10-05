/** Only the fields that differ from the values a form was opened with: a save never sends back (and so never
 * overwrites) what someone else changed meanwhile in the other fields. */
export function changedFields<T extends object>(base: T, values: T): Partial<T> {
  return Object.fromEntries(Object.entries(values).filter(([k, v]) => base[k as keyof T] !== v)) as Partial<T>
}

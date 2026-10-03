/** Returns ids with the element at index moved by delta (-1 up, +1 down); unchanged when out of range. */
export function moveId(ids: number[], index: number, delta: -1 | 1): number[] {
  const target = index + delta
  if (index < 0 || index >= ids.length || target < 0 || target >= ids.length) return ids
  const next = [...ids]
  ;[next[index], next[target]] = [next[target], next[index]]
  return next
}

import type { Dimension } from '@/api/client'
import { changedFields } from './changedFields'

/** Tags as typed in a form: comma or space separated; the server trims, lower-cases and deduplicates them. */
export function parseTags(text: string): string[] {
  return text.split(/[\s,]+/).filter((t) => t !== '')
}

export function formatTags(tags: string[]): string {
  return tags.join(', ')
}

/** The classification change a form makes: changed dimensions take their value, cleared ones null. */
export function classificationChanges(
  base: Record<string, string>,
  values: Record<string, string>,
): Record<string, string | null> {
  const out: Record<string, string | null> = {}
  for (const key of new Set([...Object.keys(base), ...Object.keys(values)])) {
    const before = base[key] ?? ''
    const after = values[key] ?? ''
    if (before !== after) out[key] = after === '' ? null : after
  }
  return out
}

/** A classification without the dimensions left empty in a form. */
export function assigned(values: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(values).filter(([, v]) => v !== ''))
}

/** "Risk: Critical" labels of a test case's classification, by the dimensions' names (keys when unknown). */
export function classificationLabels(
  classification: Record<string, string>,
  dimensions: Dimension[],
): { key: string; label: string }[] {
  return Object.entries(classification).map(([dim, value]) => {
    const d = dimensions.find((x) => x.key === dim)
    const v = d?.values.find((x) => x.key === value)
    return { key: dim, label: `${d?.name ?? dim}: ${v?.name ?? value}` }
  })
}

/** The values a form offers for a dimension: active ones, plus the current one even when archived. */
export function selectableValues(d: Dimension, current: string) {
  return d.values.filter((v) => v.archivedAt === null || v.key === current)
}

interface FormTaxonomy {
  tags: string
  classification: Record<string, string>
}

/** The request body of a new test case from form values (tags parsed, empty dimensions and those not among
 * the project's dimensions left out). */
export function createBody<T extends FormTaxonomy>(
  { tags, classification, ...rest }: T,
  dimensions: Dimension[],
) {
  const known = Object.fromEntries(
    Object.entries(assigned(classification)).filter(([k]) => dimensions.some((d) => d.key === k)),
  )
  return { ...rest, tags: parseTags(tags), classification: known }
}

/** The PATCH body of an edit: only what changed since the form was opened, so a save never overwrites what
 * someone else changed meanwhile (tags as a whole, the classification per dimension). */
export function updateBody<T extends FormTaxonomy>(base: T, values: T) {
  const { tags: baseTags, classification: baseClassification, ...baseRest } = base
  const { tags, classification, ...rest } = values
  const body: Partial<Omit<T, 'tags' | 'classification'>> & {
    tags?: string[]
    classification?: Record<string, string | null>
  } = changedFields(baseRest, rest)
  if (parseTags(tags).join(' ') !== parseTags(baseTags).join(' ')) body.tags = parseTags(tags)
  const changes = classificationChanges(baseClassification, classification)
  if (Object.keys(changes).length > 0) body.classification = changes
  return body
}

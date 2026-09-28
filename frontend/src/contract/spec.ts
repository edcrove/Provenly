import { readFileSync } from 'node:fs'
import path from 'node:path'

import Ajv, { type ValidateFunction } from 'ajv'
import addFormats from 'ajv-formats'
import { parse } from 'yaml'

/** Minimal typed view of the parts of api/openapi.yaml the contract tests need. */
interface Schema {
  [key: string]: unknown
}
interface Parameter {
  name: string
  in: 'query' | 'path'
  required?: boolean
  schema: Schema
}
interface Operation {
  operationId: string
  parameters?: (Parameter | { $ref: string })[]
  requestBody?: { content: Record<string, { schema: Schema }> }
  responses: Record<string, { $ref?: string; content?: Record<string, { schema: Schema }> }>
}
type PathItem = Record<string, Operation> & { parameters?: (Parameter | { $ref: string })[] }
export interface Spec {
  paths: Record<string, PathItem>
  components: {
    parameters: Record<string, Parameter>
    responses: Record<string, { content: Record<string, { schema: Schema }> }>
  }
}

export const specPath = path.resolve(import.meta.dirname, '../../../api/openapi.yaml')
export const spec = parse(readFileSync(specPath, 'utf8')) as Spec

const ajv = new Ajv({ strict: false, allErrors: true })
addFormats(ajv)
ajv.addFormat('int32', true)
ajv.addFormat('int64', true)
ajv.addSchema({ ...spec, $id: 'openapi' } as object)

const rewriteRefs = (s: unknown): unknown => {
  if (Array.isArray(s)) return s.map(rewriteRefs)
  if (s && typeof s === 'object') {
    return Object.fromEntries(
      Object.entries(s).map(([k, v]) => [
        k,
        k === '$ref' && typeof v === 'string' ? `openapi${v}` : rewriteRefs(v),
      ]),
    )
  }
  return s
}

const cache = new Map<string, ValidateFunction>()
function validator(schema: Schema): ValidateFunction {
  const key = JSON.stringify(schema)
  let fn = cache.get(key)
  if (!fn) {
    fn = ajv.compile(rewriteRefs(schema) as object)
    cache.set(key, fn)
  }
  return fn
}

export interface Op {
  key: string
  method: string
  path: string
  operation: Operation
  pathParameters: Parameter[]
}

const methods = ['get', 'post', 'put', 'patch', 'delete']

export function operations(): Op[] {
  const out: Op[] = []
  for (const [p, item] of Object.entries(spec.paths)) {
    for (const m of methods) {
      const operation = item[m]
      if (!operation) continue
      const params = [...(item.parameters ?? []), ...(operation.parameters ?? [])].map(resolveParam)
      out.push({
        key: `${m.toUpperCase()} ${p}`,
        method: m.toUpperCase(),
        path: p,
        operation,
        pathParameters: params,
      })
    }
  }
  return out
}

function resolveParam(p: Parameter | { $ref: string }): Parameter {
  if ('$ref' in p) return spec.components.parameters[p.$ref.split('/').pop()!]
  return p
}

/** Finds the operation serving a concrete request (method + URL path). */
export function matchOperation(
  method: string,
  urlPath: string,
): { op: Op; params: Record<string, string> } | undefined {
  for (const op of operations()) {
    if (op.method !== method) continue
    const names: string[] = []
    const re = new RegExp(
      '^' +
        op.path.replace(/\{(\w+)\}/g, (_, n: string) => {
          names.push(n)
          return '([^/]+)'
        }) +
        '$',
    )
    const m = re.exec(urlPath)
    if (m) return { op, params: Object.fromEntries(names.map((n, i) => [n, decodeURIComponent(m[i + 1])])) }
  }
  return undefined
}

function coerce(schema: Schema, raw: string): unknown {
  return schema.type === 'integer' || schema.type === 'number' ? Number(raw) : raw
}

/** Returns the contract violations of a request (empty when valid). */
export async function validateRequest(request: Request): Promise<string[]> {
  const url = new URL(request.url)
  const match = matchOperation(request.method, url.pathname)
  if (!match) return [`${request.method} ${url.pathname} is not an operation of the contract`]
  const errors: string[] = []
  for (const p of match.op.pathParameters) {
    const raw = p.in === 'path' ? match.params[p.name] : url.searchParams.get(p.name)
    if (raw === null || raw === undefined) {
      if (p.required) errors.push(`missing required ${p.in} parameter ${p.name}`)
      continue
    }
    const fn = validator(p.schema)
    if (!fn(coerce(p.schema, raw))) errors.push(`${p.in} parameter ${p.name}: ${ajv.errorsText(fn.errors)}`)
  }
  for (const name of url.searchParams.keys()) {
    if (!match.op.pathParameters.some((p) => p.in === 'query' && p.name === name))
      errors.push(`unknown query parameter ${name}`)
  }
  const body = match.op.operation.requestBody?.content['application/json']
  if (body) {
    const text = await request.clone().text()
    const fn = validator(body.schema)
    if (!fn(text ? JSON.parse(text) : undefined)) errors.push(`request body: ${ajv.errorsText(fn.errors)}`)
  }
  return errors
}

/** Returns the contract violations of a response to an operation (empty when valid). */
export async function validateResponse(op: Op, response: Response): Promise<string[]> {
  const declared = op.operation.responses[String(response.status)]
  if (!declared) return [`${op.key} does not declare status ${response.status}`]
  const resolved = declared.$ref ? spec.components.responses[declared.$ref.split('/').pop()!] : declared
  const content = resolved.content
  if (!content) {
    const text = await response.clone().text()
    return text ? [`${op.key} ${response.status} must have no body`] : []
  }
  const [mediaType, media] = Object.entries(content)[0]
  const actualType = (response.headers.get('Content-Type') ?? '').split(';')[0].trim()
  if (actualType !== mediaType)
    return [`${op.key} ${response.status}: content type ${actualType} != ${mediaType}`]
  const fn = validator(media.schema)
  return fn(await response.clone().json())
    ? []
    : [`${op.key} ${response.status}: ${ajv.errorsText(fn.errors)}`]
}

/** Declared response statuses of an operation. */
export function declaredStatuses(op: Op): number[] {
  return Object.keys(op.operation.responses).map(Number)
}

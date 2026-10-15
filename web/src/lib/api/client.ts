import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema'
import { uuid } from '../uuid'

export type Schemas = components['schemas']
export type Invoice = Schemas['Invoice']
export type InvoiceDetail = Schemas['InvoiceDetail']
export type Customer = Schemas['Customer']
export type Product = Schemas['Product']
export type Currency = Schemas['Currency']
export type BankAccount = Schemas['BankAccount']
export type Schedule = Schemas['Schedule']
export type ScheduleDetail = Schemas['ScheduleDetail']
export type EmailMessage = Schemas['EmailMessage']
export type BusinessSettings = Schemas['BusinessSettings']
export type Dashboard = Schemas['Dashboard']
export type Problem = Schemas['Problem']

/** Raised for every non-2xx response, carrying the RFC 9457 problem. */
export class ApiError extends Error {
  constructor(public problem: Problem) {
    super(problem.detail ? `${problem.title}: ${problem.detail}` : problem.title)
  }
}

let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

const csrf: Middleware = {
  onRequest({ request }) {
    request.headers.set('X-Requested-With', 'cactusinv')
    return request
  },
  onResponse({ response, request }) {
    if (response.status === 401 && !request.url.endsWith('/auth/login')) onUnauthorized()
    return response
  },
}

export const api = createClient<paths>({
  baseUrl: `${window.location.origin}/api/v1`,
  credentials: 'same-origin',
  // Resolve fetch per call so tests (and polyfills) can replace it.
  fetch: (req: Request) => globalThis.fetch(req),
})
api.use(csrf)

/** unwrap turns an openapi-fetch result into data or a thrown ApiError. */
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await p
  if (error !== undefined || !response.ok) {
    const problem = (error as Problem | undefined) ?? { type: 'about:blank', title: response.statusText, status: response.status }
    throw new ApiError(problem)
  }
  return data as T
}

/** A fresh key so a retried mutation is applied once (Idempotency-Key). */
export function idempotencyKey(): string {
  return uuid()
}

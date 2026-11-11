import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema'

export type S = components['schemas']
export type Me = S['Me']
export type User = S['User']
export type UserRef = S['UserRef']
export type Workspace = S['Workspace']
export type WorkspaceMember = S['WorkspaceMember']
export type Board = S['Board']
export type BoardFull = S['BoardFull']
export type List = S['List']
export type Card = S['Card']
export type CardDetail = S['CardDetail']
export type Label = S['Label']
export type Checklist = S['Checklist']
export type ChecklistItem = S['ChecklistItem']
export type Comment = S['Comment']
export type Attachment = S['Attachment']
export type CardLink = S['CardLink']
export type Activity = S['Activity']
export type Notification = S['Notification']
export type Recurrence = S['Recurrence']
export type Token = S['Token']
export type BoardMember = S['BoardMember']

export function csrfToken(): string {
  const m = document.cookie.match(/(?:^|;\s*)kowhai_csrf=([^;]*)/)
  return m ? decodeURIComponent(m[1]) : ''
}

export function loginURL(): string {
  const back = location.pathname + location.search
  return '/auth/login?return_to=' + encodeURIComponent(back)
}

const middleware: Middleware = {
  onRequest({ request }) {
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      request.headers.set('X-Kowhai-CSRF', csrfToken())
    }
    return request
  },
  onResponse({ response }) {
    if (response.status === 401) {
      location.assign(loginURL())
    }
    return response
  },
}

export const api = createClient<paths>({ baseUrl: '/api/v1' })
api.use(middleware)

export class ApiError extends Error {
  status: number
  constructor(status: number, title: string, detail?: string) {
    super(detail ? `${title}: ${detail}` : title)
    this.status = status
  }
}

type Result<T> = { data?: T; error?: unknown; response: Response }

// call unwraps an openapi-fetch result and throws ApiError on failure.
export async function call<T>(p: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await p
  if (!response.ok) {
    const e = (error ?? {}) as { title?: string; detail?: string }
    throw new ApiError(response.status, e.title ?? response.statusText, e.detail)
  }
  return data as T
}

// upload sends a file as multipart form data.
export async function upload(cardId: string, file: File): Promise<Attachment> {
  const fd = new FormData()
  fd.append('file', file)
  const res = await fetch(`/api/v1/cards/${cardId}/attachments`, {
    method: 'POST',
    body: fd,
    headers: { 'X-Kowhai-CSRF': csrfToken() },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, body.title ?? res.statusText, body.detail)
  return body as Attachment
}

export async function logout(): Promise<void> {
  const res = await fetch('/auth/logout', { method: 'POST', headers: { 'X-Kowhai-CSRF': csrfToken() } })
  const body = await res.json().catch(() => ({ redirect: '/' }))
  location.assign(body.redirect ?? '/')
}

// Same-origin fetch wrapper. Session is a cookie; writes carry X-CSRF-Token; 401 → login.

import { translate, translateDynamic } from '@/i18n'
import { formatDurationSec } from './format'

/**
 * A failed request. `code` is the server's machine-readable error code and
 * `serverMessage` its own wording; `message` is already translated for the
 * language that was active when the request failed. Prefer `errorText(err)` from
 * lib/errors.ts when rendering — it re-translates in the current language.
 */
export class ApiError extends Error {
  status: number
  body?: string
  code?: string
  serverMessage?: string
  /** Values the server attached for the translated sentence (e.g. retry_after). */
  params?: Record<string, unknown>
  constructor(
    status: number,
    message: string,
    body?: string,
    code?: string,
    serverMessage?: string,
    params?: Record<string, unknown>,
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
    this.code = code
    this.serverMessage = serverMessage
    this.params = params
  }
}

let csrfToken: string | null = null
export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

function readCookie(name: string): string | null {
  const escaped = name.replace(/([.$?*|{}()[\]\\/+^])/g, '\\$1')
  const m = document.cookie.match(new RegExp('(?:^|; )' + escaped + '=([^;]*)'))
  return m ? decodeURIComponent(m[1]) : null
}

function currentCsrf(): string | null {
  return csrfToken ?? readCookie('csrf') ?? readCookie('tolikopa_csrf') ?? readCookie('XSRF-TOKEN')
}

type UnauthorizedHandler = () => void
let unauthorizedHandler: UnauthorizedHandler | null = null
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  unauthorizedHandler = handler
}

// 403 `mfa_enrollment_required`: the "require 2FA" policy was switched on while
// this session was open. The handler refetches /me, whose flag sends the app to
// the enrolment screen.
let enrollmentHandler: (() => void) | null = null
export function setEnrollmentHandler(handler: (() => void) | null): void {
  enrollmentHandler = handler
}

export type QueryParams = Record<string, string | number | boolean | null | undefined>

function buildUrl(path: string, params?: QueryParams): string {
  if (!params) return path
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    sp.set(k, String(v))
  }
  const qs = sp.toString()
  return qs ? `${path}?${qs}` : path
}

/** Generic text for a status when the server gave us no usable code/message. */
export function httpMessage(status: number): string {
  switch (true) {
    case status === 0: return translate('errors.network')
    case status === 400: return translate('errors.http_400')
    case status === 401: return translate('errors.session_required')
    case status === 403: return translate('errors.http_403')
    case status === 404: return translate('errors.http_404')
    case status === 409: return translate('errors.http_409')
    case status === 413: return translate('errors.http_413')
    case status === 429: return translate('errors.http_429')
    case status >= 500: return translate('errors.http_5xx')
    default: return translate('errors.generic')
  }
}

/**
 * Errors arrive as `{code, message}`. The code is mapped to translated text
 * (`errors.<code>`); the server's own message is the fallback for a code this
 * build does not know, and beats the generic status text — a validation failure
 * such as "item not in catalog: …" is the only part that tells the admin what to
 * do.
 *
 * The message of a 5xx is deliberately dropped: `serverError` on the backend
 * already replaces the real cause with a generic message precisely so driver and
 * SQL text (table and constraint names) never reach a client, and we do not want
 * to start surfacing whatever some other 5xx path happens to emit.
 */
function parseError(
  status: number,
  body: string,
): { code?: string; message?: string; params?: Record<string, unknown> } {
  if (!body) return {}
  try {
    const parsed = JSON.parse(body) as { code?: unknown; message?: unknown; params?: unknown }
    const params =
      typeof parsed.params === 'object' && parsed.params !== null && !Array.isArray(parsed.params)
        ? (parsed.params as Record<string, unknown>)
        : undefined
    const code = typeof parsed.code === 'string' && /^[a-z0-9_]{1,64}$/.test(parsed.code) ? parsed.code : undefined
    const msg = typeof parsed.message === 'string' ? parsed.message.trim() : ''
    return { code, params, message: status < 500 && msg && msg.length <= 300 ? msg : undefined }
  } catch {
    return {}
  }
}

const DEFAULT_MIN_PASSWORD = 10
const DEFAULT_MAX_PASSWORD_BYTES = 72

/**
 * Translated sentence for a server error code, using the optional `params` the
 * server attaches. Returns undefined for a code this build does not know.
 */
export function codeMessage(code: string | undefined, params?: Record<string, unknown>): string | undefined {
  if (!code) return undefined
  const n = (key: string): number | null => {
    const v = params?.[key]
    return typeof v === 'number' && isFinite(v) ? v : null
  }
  switch (code) {
    case 'rate_limited': {
      const wait = n('retry_after')
      return wait !== null && wait > 0
        ? translate('errors.rate_limitedWait', { duration: formatDurationSec(Math.ceil(wait)) })
        : translate('errors.rate_limited')
    }
    case 'password_too_weak':
      return params?.reason === 'same_as_current'
        ? translate('errors.password_same')
        : translate('errors.password_too_weak', { min: n('min_length') ?? DEFAULT_MIN_PASSWORD })
    case 'password_too_long':
      return translate('errors.password_too_long', { max: n('max_bytes') ?? DEFAULT_MAX_PASSWORD_BYTES })
    case 'invalid_request':
      return typeof params?.field === 'string' && params.field
        ? translate('errors.invalid_requestField', { field: params.field })
        : translate('errors.invalid_request')
    case 'too_many_items': {
      const max = n('max')
      return max !== null ? translate('errors.too_many_itemsMax', { max }) : translate('errors.too_many_items')
    }
    case 'setup_incomplete': {
      // `missing` lists readiness keys and/or config field names.
      const missing = Array.isArray(params?.missing)
        ? params.missing
            .filter((m): m is string => typeof m === 'string')
            .map((m) => translateDynamic(`setup.missing.${m}`) ?? m)
        : []
      return missing.length
        ? translate('errors.setup_incompleteMissing', { missing: missing.join(', ') })
        : translate('errors.setup_incomplete')
    }
    default:
      return translateDynamic(`errors.${code}`)
  }
}

function toApiError(status: number, body: string): ApiError {
  const { code, message, params } = parseError(status, body)
  const text = codeMessage(code, params) ?? message ?? httpMessage(status)
  return new ApiError(status, text, body, code, message, params)
}

// A 401 normally means "no session" and drops the app to the login screen. These
// codes are a failed credential CHECK inside a live session (or during login) and
// must never sign the user out, whatever status the server attaches to them.
const NOT_A_SESSION_LOSS = new Set(['invalid_credentials', 'password_incorrect', 'mfa_invalid', 'mfa_token_invalid'])

interface RequestOptions {
  params?: QueryParams
  body?: unknown
  signal?: AbortSignal
  silent401?: boolean
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const isWrite = method !== 'GET' && method !== 'HEAD'
  let body: string | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  if (isWrite) {
    const csrf = currentCsrf()
    if (csrf) headers['X-CSRF-Token'] = csrf
  }

  let res: Response
  try {
    res = await fetch(buildUrl(path, opts.params), {
      method,
      credentials: 'include',
      headers,
      body,
      signal: opts.signal,
    })
  } catch (err) {
    throw new ApiError(0, translate('errors.network'), err instanceof Error ? err.message : undefined)
  }

  if (!res.ok) {
    const text = await res.text().catch(() => '')
    const error = toApiError(res.status, text)
    if (res.status === 401 && !opts.silent401 && !(error.code && NOT_A_SESSION_LOSS.has(error.code))) {
      unauthorizedHandler?.()
    }
    if (res.status === 403 && error.code === 'mfa_enrollment_required') enrollmentHandler?.()
    throw error
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
  }
  const ct = res.headers.get('content-type') ?? ''
  if (!ct.includes('application/json')) {
    return (await res.text()) as unknown as T
  }
  return (await res.json()) as T
}

/**
 * Downloads a file from an authenticated endpoint. A plain `<a download>` would
 * save the server's JSON error body as the file when the request fails, so this
 * fetches first, checks the status, and only then hands the bytes to the browser.
 * Throws ApiError (translated) on failure.
 */
async function download(path: string, params?: QueryParams, fallbackName?: string): Promise<void> {
  let res: Response
  try {
    res = await fetch(buildUrl(path, params), { credentials: 'include' })
  } catch (err) {
    throw new ApiError(0, translate('errors.network'), err instanceof Error ? err.message : undefined)
  }
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    const error = toApiError(res.status, text)
    if (res.status === 401) unauthorizedHandler?.()
    if (res.status === 403 && error.code === 'mfa_enrollment_required') enrollmentHandler?.()
    throw error
  }
  const disposition = res.headers.get('content-disposition') ?? ''
  const named = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(disposition)?.[1]
  const name = named ? decodeURIComponent(named) : (fallbackName ?? path.split('/').pop() ?? 'download')
  const url = URL.createObjectURL(await res.blob())
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  // Give the browser a moment to start the save before the blob URL goes away.
  window.setTimeout(() => URL.revokeObjectURL(url), 30_000)
}

export const api = {
  download,
  get: <T>(path: string, params?: QueryParams, signal?: AbortSignal) =>
    request<T>('GET', path, { params, signal }),
  post: <T>(path: string, body?: unknown, params?: QueryParams) =>
    request<T>('POST', path, { body, params }),
  put: <T>(path: string, body?: unknown, params?: QueryParams) =>
    request<T>('PUT', path, { body, params }),
  patch: <T>(path: string, body?: unknown, params?: QueryParams) =>
    request<T>('PATCH', path, { body, params }),
  del: <T>(path: string, params?: QueryParams) =>
    request<T>('DELETE', path, { params }),
  request,
}

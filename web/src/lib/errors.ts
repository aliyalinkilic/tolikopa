// Turns anything thrown by a request into a sentence for the user, in the language
// active right now (ApiError.message was translated when the request failed).
import { translate } from '@/i18n'
import type { TKey } from '@/i18n'
import { ApiError, codeMessage, httpMessage } from './api'

/** The server's error code, if this is an API error that carried one. */
export function errorCode(err: unknown): string | undefined {
  return err instanceof ApiError ? err.code : undefined
}

/**
 * code → `errors.<code>`; else the server's own message; else generic text for the
 * HTTP status; else `fallback`.
 */
export function errorText(err: unknown, fallback: TKey = 'errors.generic'): string {
  if (err instanceof ApiError) {
    return codeMessage(err.code, err.params) ?? err.serverMessage ?? (err.status === 0 || err.status >= 400 ? httpMessage(err.status) : translate(fallback))
  }
  return translate(fallback)
}

import { reportUnauthorized } from '@/api/client'
import { ApiError, errorMessage } from '@/api/errors'

// Reports a lost session and returns null; otherwise returns the message to show inline.
export function inlineError(e: unknown): string | null {
  if (e instanceof ApiError && e.code === 'unauthorized') {
    reportUnauthorized()
    return null
  }
  return errorMessage(e)
}

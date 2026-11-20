import { HttpResponse } from 'msw'
import { setupServer } from 'msw/node'

export const server = setupServer()

export function json(status: number, body: unknown) {
  return HttpResponse.json(body as Record<string, unknown>, { status })
}

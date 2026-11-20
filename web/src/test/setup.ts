import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './server'

// Node's Request rejects relative URLs; resolve them against the jsdom origin like a browser does.
const NativeRequest = globalThis.Request
globalThis.Request = class extends NativeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, location.origin).href : input, init)
  }
}

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

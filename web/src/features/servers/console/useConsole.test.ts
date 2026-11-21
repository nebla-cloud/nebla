// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { withSetup } from '@/test/mount'
import { useConsole } from './useConsole'

function grants(...urls: string[]) {
  let n = 0
  const calls: string[] = []
  server.use(
    http.post('*/v1/servers/1/actions/request_console', ({ request }) => {
      calls.push(request.url)
      const url = urls[Math.min(n, urls.length - 1)]
      n++
      return json(201, { wss_url: url, password: `pw${n}` })
    }),
  )
  return calls
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useConsole', () => {
  it('reconnect requests a fresh grant', async () => {
    const calls = grants('wss://panel/v1/console/a', 'wss://panel/v1/console/b')
    const { result } = withSetup(() => useConsole(ref(1)))

    await result.connect()
    expect(result.grant.value?.wss_url).toBe('wss://panel/v1/console/a')
    result.opened()
    expect(result.state.value).toBe('connected')

    result.closed()
    expect(result.state.value).toBe('disconnected')
    expect(result.grant.value).toBeNull()

    await result.connect()
    expect(calls).toHaveLength(2)
    expect(result.grant.value?.wss_url).toBe('wss://panel/v1/console/b')
    expect(result.grant.value?.password).toBe('pw2')
  })

  it('grant is used immediately', async () => {
    grants('wss://panel/v1/console/a')
    const { result } = withSetup(() => useConsole(ref(1)))
    vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval'] })

    const pending = result.connect()
    expect(result.state.value).toBe('connecting')
    await pending
    // No timer stands between the grant and the url the screen reads.
    expect(vi.getTimerCount()).toBe(0)
    expect(result.grant.value?.wss_url).toBe('wss://panel/v1/console/a')
    expect(result.state.value).toBe('connecting')
  })

  it('failed grant shows error', async () => {
    server.use(
      http.post('*/v1/servers/1/actions/request_console', () =>
        json(409, { error: { code: 'conflict', message: "web1 isn't running." } }),
      ),
    )
    const { result } = withSetup(() => useConsole(ref(1)))
    await result.connect()
    expect(result.state.value).toBe('failed')
    expect(result.error.value).toBe("web1 isn't running.")
    expect(result.grant.value).toBeNull()
  })

  it('a grant that arrives after disconnect is dropped', async () => {
    grants('wss://panel/v1/console/a')
    const { result } = withSetup(() => useConsole(ref(1)))
    const pending = result.connect()
    result.disconnect()
    await pending
    expect(result.grant.value).toBeNull()
    expect(result.state.value).toBe('disconnected')
  })

  it('closing before connecting is a failure', async () => {
    grants('wss://panel/v1/console/a')
    const { result } = withSetup(() => useConsole(ref(1)))
    await result.connect()
    result.closed()
    expect(result.state.value).toBe('failed')
    expect(result.error.value).toBeTruthy()
  })
})

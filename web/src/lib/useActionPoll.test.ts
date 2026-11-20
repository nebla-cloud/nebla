import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { withSetup } from '@/test/mount'
import { useActionPoll } from './useActionPoll'

const action = (status: string, progress: number, error: object | null = null) => ({
  id: 5,
  command: 'create_server',
  status,
  progress,
  resources: [{ id: 12, type: 'server' }],
  error,
  started: '2026-11-18T10:00:00Z',
  finished: status === 'running' ? null : '2026-11-18T10:01:00Z',
})

function sequence(steps: object[]) {
  let calls = 0
  server.use(
    http.get('*/v1/actions/5', () => {
      const body = steps[Math.min(calls, steps.length - 1)]
      calls++
      return json(200, body)
    }),
  )
  return () => calls
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe('useActionPoll', () => {
  it('polls until success', async () => {
    const calls = sequence([action('running', 10), action('running', 60), action('success', 100)])
    const onDone = vi.fn()
    const { result, queryClient } = withSetup(() =>
      useActionPoll(ref(5), { onDone, intervalMs: 10 }),
    )
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await vi.waitFor(() => expect(onDone).toHaveBeenCalled())
    await sleep(50)
    expect(result.running.value).toBe(false)
    expect(result.action.value?.status).toBe('success')
    expect(onDone).toHaveBeenCalledOnce()
    expect(calls()).toBe(3)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['servers'] })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['server', 12] })
  })

  it('stops and surfaces error', async () => {
    const calls = sequence([
      action('running', 10),
      action('error', 40, { code: 'pve_error', message: 'clone failed' }),
    ])
    const onDone = vi.fn()
    const { result } = withSetup(() => useActionPoll(ref(5), { onDone, intervalMs: 10 }))
    await vi.waitFor(() => expect(result.error.value).toBe('clone failed'))
    const seen = calls()
    await sleep(60)
    expect(calls()).toBe(seen)
    expect(result.running.value).toBe(false)
    expect(onDone).not.toHaveBeenCalled()
  })

  it('stops polling on a 4xx', async () => {
    let calls = 0
    server.use(
      http.get('*/v1/actions/5', () => {
        calls++
        return calls === 1
          ? json(200, action('running', 10))
          : json(404, { error: { code: 'not_found', message: 'action not found' } })
      }),
    )
    const { result } = withSetup(() => useActionPoll(ref(5), { intervalMs: 10 }))
    await vi.waitFor(() => expect(result.error.value).toBe('action not found'))
    const seen = calls
    await sleep(60)
    expect(calls).toBe(seen)
  })

  it('does nothing without an action id', async () => {
    const calls = sequence([action('running', 10)])
    const { result } = withSetup(() => useActionPoll(ref(null)))
    await sleep(20)
    expect(calls()).toBe(0)
    expect(result.running.value).toBe(false)
  })
})

import { onScopeDispose, ref, type Ref } from 'vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { components } from '@/api/schema'

export type ConsoleGrant = components['schemas']['ConsoleGrant']
export type ConsoleState = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'failed'

export function useConsole(serverId: Ref<number>) {
  const state = ref<ConsoleState>('idle')
  const error = ref<string | null>(null)
  const grant = ref<ConsoleGrant | null>(null)
  // Bumped on every connect and disconnect so a late grant is dropped.
  let attempt = 0

  async function connect(): Promise<void> {
    const mine = ++attempt
    state.value = 'connecting'
    error.value = null
    grant.value = null
    try {
      const res = await api.POST('/servers/{id}/actions/request_console', {
        params: { path: { id: serverId.value } },
      })
      const g = unwrap(res)
      if (mine !== attempt) return
      // The token lives 10 seconds and is single-use: hand it to the screen now.
      grant.value = g
    } catch (e) {
      if (mine !== attempt) return
      state.value = 'failed'
      error.value = errorMessage(e)
    }
  }

  function disconnect(): void {
    attempt++
    grant.value = null
    if (state.value !== 'idle') state.value = 'disconnected'
  }

  function opened(): void {
    if (grant.value) state.value = 'connected'
  }

  function closed(): void {
    if (state.value === 'connecting' && grant.value) {
      fail('The console closed before it connected. Try again in a moment.')
      return
    }
    disconnect()
  }

  function fail(message: string): void {
    attempt++
    grant.value = null
    state.value = 'failed'
    error.value = message
  }

  onScopeDispose(disconnect)

  return { state, error, grant, connect, disconnect, opened, closed, fail }
}

export type ConsoleHandle = ReturnType<typeof useConsole>

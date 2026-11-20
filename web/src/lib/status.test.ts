import { describe, expect, it } from 'vitest'
import { isTransitional, serverStatus } from './status'

describe('serverStatus', () => {
  it('maps every status', () => {
    expect(serverStatus('running')).toEqual({ label: 'Running', tone: 'success', pulse: false })
    expect(serverStatus('off')).toEqual({ label: 'Off', tone: 'neutral', pulse: false })
    expect(serverStatus('rebuilding')).toEqual({
      label: 'Rebuilding',
      tone: 'neutral',
      pulse: true,
    })
    expect(serverStatus('missing')).toEqual({ label: 'Missing', tone: 'error', pulse: false })
    expect(serverStatus('error')).toEqual({ label: 'Error', tone: 'error', pulse: false })
  })

  it('isTransitional follows pulse', () => {
    for (const s of ['initializing', 'starting', 'stopping', 'rebuilding', 'deleting'] as const) {
      expect(isTransitional(s)).toBe(true)
    }
    expect(isTransitional('running')).toBe(false)
    expect(isTransitional('error')).toBe(false)
  })
})

import { describe, expect, it } from 'vitest'
import { isTransitional, isVolumeTransitional, serverStatus, volumeStatus } from './status'

describe('serverStatus', () => {
  it('maps every status', () => {
    expect(serverStatus('running')).toEqual({ label: 'Running', tone: 'success', pulse: false })
    expect(serverStatus('off')).toEqual({ label: 'Off', tone: 'neutral', pulse: false })
    expect(serverStatus('rebuilding')).toEqual({
      label: 'Rebuilding',
      tone: 'busy',
      pulse: true,
    })
    expect(serverStatus('missing')).toEqual({ label: 'Missing', tone: 'error', pulse: false })
    expect(serverStatus('error')).toEqual({ label: 'Error', tone: 'error', pulse: false })
  })

  it('uses the busy tone for transitional statuses', () => {
    expect(serverStatus('starting').tone).toBe('busy')
    expect(serverStatus('running').tone).toBe('success')
  })

  it('isTransitional follows pulse', () => {
    for (const s of ['initializing', 'starting', 'stopping', 'rebuilding', 'deleting'] as const) {
      expect(isTransitional(s)).toBe(true)
    }
    expect(isTransitional('running')).toBe(false)
    expect(isTransitional('error')).toBe(false)
  })
})

describe('volumeStatus', () => {
  it('maps volume statuses', () => {
    expect(volumeStatus('attached')).toEqual({ label: 'Attached', tone: 'success', pulse: false })
    expect(volumeStatus('available')).toEqual({ label: 'Not attached', tone: 'neutral', pulse: false })
    expect(volumeStatus('creating').tone).toBe('busy')
    expect(volumeStatus('error').tone).toBe('error')
  })
  it('polls only while creating or deleting', () => {
    expect(isVolumeTransitional('creating')).toBe(true)
    expect(isVolumeTransitional('deleting')).toBe(true)
    expect(isVolumeTransitional('attached')).toBe(false)
  })
})

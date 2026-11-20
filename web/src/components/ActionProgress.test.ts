import { describe, expect, it } from 'vitest'
import { actionLabel } from './actionLabel'

describe('actionLabel', () => {
  it('names known commands and falls back to sentence case', () => {
    expect(actionLabel('reboot_server')).toBe('Rebooting')
    expect(actionLabel('create_server')).toBe('Creating')
    expect(actionLabel('sync_location_nodes')).toBe('Sync location nodes')
  })
})

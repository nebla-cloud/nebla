import { describe, expect, it } from 'vitest'
import { actionDone, actionLabel, actionTitle } from './actionLabel'

describe('actionLabel', () => {
  it('actionDone covers every server command', () => {
    const cases: [string, string][] = [
      ['create_server', 'created'],
      ['start_server', 'started'],
      ['stop_server', 'powered off'],
      ['shutdown_server', 'shut down'],
      ['reboot_server', 'rebooted'],
      ['reset_server', 'reset'],
      ['rebuild_server', 'rebuilt'],
      ['change_server_type', 'resized'],
      ['delete_server', 'deleted'],
    ]
    for (const [command, verb] of cases) expect(actionDone(command)).toBe(verb)
    expect(actionDone('attach_iso')).toBeNull()
  })

  it('actionTitle falls back to sentence case', () => {
    expect(actionTitle('rebuild_server')).toBe('Rebuild')
    expect(actionTitle('change_server_type')).toBe('Resize')
    expect(actionTitle('create_server')).toBe('Create')
    expect(actionTitle('attach_iso_image')).toBe('Attach iso image')
  })

  it('actionLabel keeps the progress wording', () => {
    expect(actionLabel('reboot_server')).toBe('Rebooting')
    expect(actionLabel('change_server_type')).toBe('Changing type')
    expect(actionLabel('attach_iso')).toBe('Attach iso')
  })
})

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
    expect(actionLabel('change_server_type')).toBe('Resizing')
    expect(actionLabel('attach_iso')).toBe('Attach iso')
    expect(actionLabel('attach_volume')).toBe('Attaching volume')
  })

  it('labels the snapshot commands', () => {
    expect(actionLabel('create_snapshot')).toBe('Taking snapshot')
    expect(actionLabel('delete_snapshot')).toBe('Deleting snapshot')
    expect(actionTitle('create_snapshot')).toBe('Take snapshot')
    expect(actionTitle('delete_snapshot')).toBe('Delete snapshot')
  })
})

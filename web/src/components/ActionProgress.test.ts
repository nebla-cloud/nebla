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
import { actionLabel } from './actionLabel'

describe('actionLabel', () => {
  it('names known commands and falls back to sentence case', () => {
    expect(actionLabel('reboot_server')).toBe('Rebooting')
    expect(actionLabel('create_server')).toBe('Creating')
    expect(actionLabel('sync_location_nodes')).toBe('Sync location nodes')
  })
})

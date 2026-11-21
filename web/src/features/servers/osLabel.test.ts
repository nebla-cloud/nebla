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
import { osLabel, osParts } from './osLabel'

const img = (os_flavor: string, os_version: string, name = 'slug-1') => ({
  name,
  os_flavor,
  os_version,
})

describe('osLabel', () => {
  it.each([
    ['debian', '13', 'Debian 13'],
    ['ubuntu', '24.04', 'Ubuntu 24.04'],
    ['rocky', '9', 'Rocky Linux 9'],
    ['alma', '9.4', 'AlmaLinux 9.4'],
    ['fedora', '41', 'Fedora 41'],
  ])('%s %s reads %s', (flavor, version, want) => {
    expect(osLabel(img(flavor, version))).toBe(want)
  })

  it('ignores case and stray spaces in the flavor', () => {
    expect(osLabel(img(' Debian ', '13'))).toBe('Debian 13')
  })

  it('falls back to the image name for an unknown flavor', () => {
    expect(osLabel(img('arch', 'rolling', 'arch-linux'))).toBe('arch-linux')
  })

  it('drops an empty version', () => {
    expect(osLabel(img('debian', ''))).toBe('Debian')
  })
})

describe('osParts', () => {
  it('splits a known flavor into name and version', () => {
    expect(osParts(img('ubuntu', '24.04'))).toEqual({ name: 'Ubuntu', version: '24.04' })
  })

  it('is null for an unknown flavor', () => {
    expect(osParts(img('nixos', '24.11'))).toBeNull()
  })
})

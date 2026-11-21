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
import { tokenSlug } from './slug'

describe('tokenSlug', () => {
  it('lowercases and dashes runs of other characters', () => {
    expect(tokenSlug('My Homelab!')).toBe('my-homelab')
    expect(tokenSlug('Home lab')).toBe('home-lab')
  })
  it('falls back to project', () => {
    expect(tokenSlug('!!!')).toBe('project')
  })
})

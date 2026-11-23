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
import {
  floatingIpStatus,
  isFloatingIpTransitional,
  isTransitional,
  isVolumeTransitional,
  serverStatus,
  volumeStatus,
} from './status'

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

describe('floatingIpStatus', () => {
  it('maps floating IP statuses', () => {
    expect(floatingIpStatus('active')).toEqual({ label: 'Active', tone: 'success', pulse: false })
    expect(floatingIpStatus('releasing')).toEqual({ label: 'Releasing', tone: 'busy', pulse: true })
    expect(floatingIpStatus('deleting')).toEqual({ label: 'Deleting', tone: 'busy', pulse: true })
  })
  it('polls only while releasing or deleting', () => {
    expect(isFloatingIpTransitional('releasing')).toBe(true)
    expect(isFloatingIpTransitional('deleting')).toBe(true)
    expect(isFloatingIpTransitional('active')).toBe(false)
  })
})

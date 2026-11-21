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

import { afterEach, describe, expect, it } from 'vitest'
import { mountApp } from '@/test/mount'
import CreateSummary from './CreateSummary.vue'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('CreateSummary', () => {
  it('writes quota lines without arrows', () => {
    const { wrapper } = mountApp(CreateSummary, {
      props: {
        formId: 'f',
        ipv4: true,
        ipv6: false,
        keyCount: 0,
        quota: [
          { label: 'Servers', used: 3, after: 4, max: 10 },
          { label: 'vCPU', used: 6, after: 8, max: 0 },
          { label: 'Memory', used: 12288, after: 16384, max: 65536 },
        ],
        blocker: null,
        error: null,
        submitting: false,
        name: 'web1',
      },
    })
    const lines = wrapper.findAll('li').map((l) => l.text())
    expect(lines).toEqual([
      'Servers: 3 of 10, 4 after this',
      'vCPU: 6, no limit',
      'Memory: 12 GB of 64 GB, 16 GB after this',
    ])
    const meters = wrapper.findAll('[role="meter"]')
    expect(
      meters.map((m) => [
        m.attributes('aria-label'),
        m.attributes('aria-valuenow'),
        m.attributes('aria-valuemax'),
      ]),
    ).toEqual([
      ['Servers', '4', '10'],
      ['Memory', '16384', '65536'],
    ])
  })

  it('sits in a panel with the name input and Create button', () => {
    const { wrapper } = mountApp(CreateSummary, {
      props: {
        formId: 'f',
        ipv4: true,
        ipv6: false,
        keyCount: 1,
        quota: [],
        blocker: null,
        error: null,
        submitting: false,
        name: 'web1',
      },
    })
    const aside = wrapper.get('aside[aria-label="Summary"]')
    expect(aside.find('h2.nb-h2').text()).toBe('Summary')
    expect(aside.find('input[name="name"]').exists()).toBe(true)
    expect(aside.find('button[type="submit"]').text()).toBe('Create server')
    expect(aside.text()).toContain('1 key')
  })

  it('names the image by its OS, not its slug', () => {
    const { wrapper } = mountApp(CreateSummary, {
      props: {
        formId: 'f',
        image: {
          id: 3,
          name: 'debian-13',
          description: '',
          os_flavor: 'debian',
          os_version: '13',
          locations: [1],
        },
        ipv4: true,
        ipv6: false,
        keyCount: 0,
        quota: [],
        blocker: null,
        error: null,
        submitting: false,
        name: 'web1',
      },
    })
    const dds = wrapper.findAll('dd').map((d) => d.text())
    expect(dds).toContain('Debian 13')
    expect(dds).not.toContain('debian-13')
  })
})

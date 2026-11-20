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
  })
})

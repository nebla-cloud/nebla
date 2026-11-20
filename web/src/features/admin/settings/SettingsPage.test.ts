import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import { resetSession } from '@/lib/session'
import SettingsPage from './SettingsPage.vue'

let puts: unknown[] = []
let manifestCalls = 0

function serve(selfSignup: boolean, putStatus = 200) {
  server.use(
    http.get('*/v1/admin/settings', () => json(200, { self_signup: selfSignup })),
    http.put('*/v1/admin/settings', async ({ request }) => {
      puts.push(await request.json())
      return putStatus === 200
        ? json(200, { self_signup: true })
        : json(500, { error: { code: 'internal', message: 'Could not save the setting.' } })
    }),
    http.get('*/v1/ui/manifest', () => {
      manifestCalls++
      return json(200, {
        setup_required: false,
        signup_enabled: true,
        branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
        admin_sections: [],
      })
    }),
    http.get('*/v1/me', () =>
      json(200, { id: 1, name: 'Ada', email: 'ada@example.com', is_admin: true }),
    ),
  )
}

async function ready(w: ReturnType<typeof mountApp>['wrapper']) {
  await vi.waitFor(() => expect(w.find('button[role="switch"]').exists()).toBe(true))
}

const toggle = (w: ReturnType<typeof mountApp>['wrapper']) => w.find('button[role="switch"]')

beforeEach(() => {
  document.body.innerHTML = ''
  puts = []
  manifestCalls = 0
  resetSession()
})

describe('SettingsPage', () => {
  it('the switch reflects self_signup', async () => {
    serve(true)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    expect(wrapper.text()).toContain('Let anyone create an account')
    expect(toggle(wrapper).attributes('aria-checked')).toBe('true')
  })

  it('toggling saves, shows a toast and reloads the manifest', async () => {
    serve(false)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('false')
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(document.body.textContent).toContain('Sign-ups are on'))
    expect(puts).toEqual([{ self_signup: true }])
    expect(manifestCalls).toBe(1)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('true')
  })

  it('a failed save reverts the switch and shows the error', async () => {
    serve(false, 500)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() =>
      expect(document.body.textContent).toContain('Could not save the setting.'),
    )
    expect(manifestCalls).toBe(0)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('false')
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { http } from 'msw'
import type { VueWrapper } from '@vue/test-utils'
import { useToast } from '@nuxt/ui/composables/useToast'
import { json, server } from '@/test/server'
import { mountApp as mountRaw } from '@/test/mount'
import { resetSession } from '@/lib/session'
import SettingsPage from './SettingsPage.vue'

let puts: unknown[] = []
let manifestCalls = 0
let wrappers: VueWrapper[] = []

function mountApp(c: typeof SettingsPage) {
  const m = mountRaw(c)
  wrappers.push(m.wrapper)
  return m
}

function serve(selfSignup: boolean, putStatus = 200, manifestStatus = 200) {
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
      if (manifestStatus !== 200) {
        return json(manifestStatus, { error: { code: 'internal', message: 'Down.' } })
      }
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

async function ready(w: VueWrapper) {
  await vi.waitFor(() => expect(w.find('button[role="switch"]').exists()).toBe(true))
}

const toggle = (w: VueWrapper) => w.find('button[role="switch"]')

afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
  useToast().clear()
  document.body.innerHTML = ''
})

beforeEach(() => {
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

  it('a saved setting toasts success even if the manifest reload fails', async () => {
    serve(false, 200, 500)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(document.body.textContent).toContain('Sign-ups are on'))
    await vi.waitFor(() => expect(manifestCalls).toBe(1))
    expect(document.body.textContent).not.toContain('Down.')
  })

  it('has a one-line explainer', async () => {
    serve(true)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    expect(wrapper.text()).toContain('Panel-wide options that apply to everyone.')
  })

  it('toggling saves, shows a toast and reloads the manifest', async () => {
    serve(false)
    const { wrapper } = mountApp(SettingsPage)
    await ready(wrapper)
    expect(toggle(wrapper).attributes('aria-checked')).toBe('false')
    await toggle(wrapper).trigger('click')
    await vi.waitFor(() => expect(document.body.textContent).toContain('Sign-ups are on'))
    expect(puts).toEqual([{ self_signup: true }])
    await vi.waitFor(() => expect(manifestCalls).toBe(1))
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

import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { resetSession, useSession } from '@/lib/session'
import LoginPage from './LoginPage.vue'

const user = { id: 1, email: 'kai@example.com', name: 'Kai', is_admin: false, totp_enabled: true }

async function loadSession() {
  server.use(
    http.get('*/v1/ui/manifest', () =>
      json(200, {
        setup_required: false,
        signup_enabled: false,
        branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
        admin_sections: [],
      }),
    ),
    http.get('*/v1/me', () => json(401, { error: { code: 'unauthorized', message: 'no' } })),
  )
  await useSession().refresh()
}

beforeEach(() => {
  resetSession()
  document.body.innerHTML = ''
})

describe('LoginPage', () => {
  it('asks for TOTP on totp_required', async () => {
    await loadSession()
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('*/v1/auth/login', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        if (!body.totp_code) {
          return json(401, { error: { code: 'totp_required', message: 'a TOTP code is required' } })
        }
        return json(200, user)
      }),
    )
    const router = testRouter([{ path: '/login', name: 'login', component: LoginPage }])
    await router.push('/login?next=/p/4/servers')
    const { wrapper } = mountApp(LoginPage, { router })
    await wrapper.find('input[name="email"]').setValue('kai@example.com')
    await wrapper.find('input[name="password"]').setValue('hunter2hunter2')
    expect(wrapper.find('input[name="totp_code"]').exists()).toBe(false)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    expect(wrapper.find('input[name="totp_code"]').exists()).toBe(true)

    await wrapper.find('input[name="totp_code"]').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    expect(bodies).toHaveLength(2)
    expect(bodies[1]).toEqual({
      email: 'kai@example.com',
      password: 'hunter2hunter2',
      totp_code: '123456',
    })
    expect(useSession().me.value).toMatchObject({ email: 'kai@example.com' })
    expect(router.currentRoute.value.fullPath).toBe('/p/4/servers')
  })
})

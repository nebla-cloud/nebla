import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { resetSession, useSession } from '@/lib/session'
import SetupPage from './SetupPage.vue'

beforeEach(() => {
  resetSession()
  document.body.innerHTML = ''
})

describe('SetupPage', () => {
  it('creates admin then logs in', async () => {
    const calls: { path: string; body: unknown }[] = []
    let setupDone = false
    const user = {
      id: 1,
      email: 'kai@example.com',
      name: 'Kai',
      is_admin: true,
      totp_enabled: false,
    }
    server.use(
      http.get('*/v1/ui/manifest', () =>
        json(200, {
          setup_required: !setupDone,
          signup_enabled: false,
          branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
          admin_sections: [],
        }),
      ),
      http.get('*/v1/me', () =>
        calls.some((c) => c.path === 'login')
          ? json(200, user)
          : json(401, { error: { code: 'unauthorized', message: 'no' } }),
      ),
      http.post('*/v1/setup', async ({ request }) => {
        calls.push({ path: 'setup', body: await request.json() })
        setupDone = true
        return json(201, user)
      }),
      http.post('*/v1/auth/login', async ({ request }) => {
        calls.push({ path: 'login', body: await request.json() })
        return json(200, user)
      }),
    )
    await useSession().refresh()
    const router = testRouter([{ path: '/setup', name: 'setup', component: SetupPage }])
    await router.push('/setup')
    const { wrapper } = mountApp(SetupPage, { router })
    await wrapper.find('input[name="name"]').setValue('Kai')
    await wrapper.find('input[name="email"]').setValue('kai@example.com')
    await wrapper.find('input[name="password"]').setValue('correct horse battery')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    await flushPromises()

    expect(calls.map((c) => c.path)).toEqual(['setup', 'login'])
    expect(calls[0]!.body).toEqual({
      name: 'Kai',
      email: 'kai@example.com',
      password: 'correct horse battery',
    })
    expect(calls[1]!.body).toEqual({ email: 'kai@example.com', password: 'correct horse battery' })
    expect(useSession().manifest.value?.setup_required).toBe(false)
    expect(useSession().me.value?.is_admin).toBe(true)
    expect(router.currentRoute.value.fullPath).toBe('/')
  })
})

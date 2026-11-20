import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { resetSession, useSession } from '@/lib/session'
import SignupPage from './SignupPage.vue'

const user = { id: 2, email: 'new@example.com', name: 'New', is_admin: false, totp_enabled: false }

beforeEach(() => {
  resetSession()
  document.body.innerHTML = ''
})

async function fill(wrapper: ReturnType<typeof mountApp>['wrapper'], password: string) {
  await wrapper.find('input[name="name"]').setValue('New')
  await wrapper.find('input[name="email"]').setValue('new@example.com')
  await wrapper.find('input[name="password"]').setValue(password)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
  await flushPromises()
}

function setup() {
  const router = testRouter([
    { path: '/login', name: 'login', component: SignupPage },
    { path: '/signup', name: 'signup', component: SignupPage },
  ])
  return mountApp(SignupPage, { router })
}

describe('SignupPage', () => {
  it('creates account and goes home', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/signup', async ({ request }) => {
        body = await request.json()
        return json(201, user)
      }),
    )
    const { wrapper, router } = setup()
    await router.push('/signup')
    await fill(wrapper, 'long-enough-pw')
    expect(body).toEqual({ name: 'New', email: 'new@example.com', password: 'long-enough-pw' })
    expect(useSession().me.value).toMatchObject({ email: 'new@example.com' })
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('short password blocked locally', async () => {
    let called = false
    server.use(
      http.post('*/v1/signup', () => {
        called = true
        return json(201, user)
      }),
    )
    const { wrapper } = setup()
    await fill(wrapper, 'short')
    expect(called).toBe(false)
    expect(wrapper.text()).toContain('at least 10 characters')
  })

  it('email_taken links to login', async () => {
    server.use(
      http.post('*/v1/signup', () =>
        json(409, { error: { code: 'email_taken', message: 'That email is already registered.' } }),
      ),
    )
    const { wrapper } = setup()
    await fill(wrapper, 'long-enough-pw')
    expect(wrapper.text()).toContain('already registered')
    expect(wrapper.find('a[href="/login"]').exists()).toBe(true)
  })
})

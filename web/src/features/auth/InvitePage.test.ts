import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import { onUnauthorized } from '@/api/client'
import { resetSession, setMe, useSession } from '@/lib/session'
import InvitePage from './InvitePage.vue'

const preview = {
  project_name: 'Homelab',
  email: 'sam@example.com',
  role: 'member',
  inviter_name: 'Sam Reed',
  expires: '2030-01-01T00:00:00Z',
  account_exists: false,
  can_create_account: false,
}
const sam = { id: 1, email: 'sam@example.com', name: 'Sam', is_admin: false, totp_enabled: false }
const project = { id: 7, name: 'Homelab', role: 'member' }

function usePreview(over: Record<string, unknown> = {}) {
  server.use(http.get('*/v1/invites/tok', () => json(200, { ...preview, ...over })))
}

async function open(me: typeof sam | null) {
  resetSession()
  server.use(
    http.get('*/v1/ui/manifest', () =>
      json(200, {
        setup_required: false,
        signup_enabled: false,
        branding: { product_name: 'Nebla', logo_url: '', primary_color: '' },
        admin_sections: [],
      }),
    ),
    http.get('*/v1/me', () =>
      me ? json(200, me) : json(401, { error: { code: 'unauthorized', message: 'no' } }),
    ),
  )
  await useSession().refresh()
  setMe(me)
  const router = testRouter([
    { path: '/invite/:token', name: 'invite', component: InvitePage },
    { path: '/login', name: 'login', component: InvitePage },
  ])
  await router.push('/invite/tok')
  const m = mountApp(InvitePage, { router })
  await flushPromises()
  await flushPromises()
  return m
}

beforeEach(() => {
  document.body.innerHTML = ''
})

const button = (w: Awaited<ReturnType<typeof open>>['wrapper'], text: string) =>
  w.findAll('button, a').find((b) => b.text().includes(text))

describe('InvitePage', () => {
  it('joins when the email matches', async () => {
    usePreview()
    let hit = 0
    server.use(
      http.post('*/v1/invites/tok/accept', () => {
        hit++
        return json(200, project)
      }),
    )
    const { wrapper, router } = await open(sam)
    expect(wrapper.text()).toContain('Join Homelab')
    expect(wrapper.text()).toContain('Sam Reed invited you as a member.')
    await button(wrapper, 'Join')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(hit).toBe(1)
    expect(router.currentRoute.value.fullPath).toBe('/p/7/servers')
  })

  it('other email offers log out', async () => {
    usePreview({ email: 'alex@example.com' })
    let accepted = false
    server.use(
      http.post('*/v1/invites/tok/accept', () => {
        accepted = true
        return json(200, project)
      }),
      http.post('*/v1/auth/logout', () => new Response(null, { status: 204 })),
    )
    const { wrapper, router } = await open(sam)
    expect(wrapper.text()).toContain('alex@example.com')
    expect(wrapper.text()).toContain('sam@example.com')
    await button(wrapper, 'Log out')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(accepted).toBe(false)
    expect(useSession().me.value).toBeNull()
    expect(router.currentRoute.value.fullPath).toBe('/invite/tok')
    expect(wrapper.text()).toContain('Join Homelab')
  })

  it('read-only invites say read-only access', async () => {
    usePreview({ role: 'read_only' })
    const { wrapper } = await open(sam)
    expect(wrapper.text()).toContain('Sam Reed invited you with read-only access.')
  })

  it('log out from the other-email state clears the query cache', async () => {
    usePreview({ email: 'alex@example.com' })
    server.use(http.post('*/v1/auth/logout', () => new Response(null, { status: 204 })))
    const { wrapper, queryClient } = await open(sam)
    queryClient.setQueryData(['projects'], [{ id: 1 }])
    await button(wrapper, 'Log out')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(queryClient.getQueryData(['projects'])).toBeUndefined()
  })

  it('login_required on join reports a lost session', async () => {
    usePreview()
    server.use(
      http.post('*/v1/invites/tok/accept', () =>
        json(401, { error: { code: 'login_required', message: 'Log in first.' } }),
      ),
    )
    const fn = vi.fn()
    onUnauthorized(fn)
    const { wrapper } = await open(sam)
    await button(wrapper, 'Join')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(fn).toHaveBeenCalledOnce()
    onUnauthorized(() => {})
  })

  it('account_exists links to login even when it can create', async () => {
    usePreview({ account_exists: true, can_create_account: true })
    const { wrapper } = await open(null)
    const link = button(wrapper, 'Log in to accept')!
    expect(link.attributes('href')).toBe('/login?next=/invite/tok')
    expect(wrapper.find('input[name="password"]').exists()).toBe(false)
  })

  it('creates an account and joins', async () => {
    usePreview({ can_create_account: true })
    let body: unknown
    server.use(
      http.post('*/v1/invites/tok/accept', async ({ request }) => {
        body = await request.json()
        return json(200, project)
      }),
    )
    const { wrapper, router } = await open(null)
    await wrapper.find('input[name="name"]').setValue('Sam')
    await wrapper.find('input[name="password"]').setValue('short')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(body).toBeUndefined()
    expect(wrapper.text()).toContain('at least 10 characters')

    server.use(http.get('*/v1/me', () => json(200, sam)))
    await wrapper.find('input[name="password"]').setValue('long-enough-pw')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await flushPromises()
    await flushPromises()
    expect(body).toEqual({ name: 'Sam', password: 'long-enough-pw' })
    expect(useSession().me.value).toMatchObject({ email: 'sam@example.com' })
    expect(router.currentRoute.value.fullPath).toBe('/p/7/servers')
  })

  it('tells you to ask an admin when no account can be made', async () => {
    usePreview()
    const { wrapper } = await open(null)
    expect(wrapper.text()).toContain(
      'Ask the person who invited you to have an admin create your account.',
    )
  })

  it('404 state', async () => {
    server.use(
      http.get('*/v1/invites/tok', () => json(404, { error: { code: 'not_found', message: 'x' } })),
    )
    const { wrapper } = await open(null)
    expect(wrapper.text()).toContain('This invite link no longer works. Ask for a new one.')
  })

  it('already_member goes to the project', async () => {
    usePreview()
    server.use(
      http.post('*/v1/invites/tok/accept', () =>
        json(409, { error: { code: 'already_member', message: 'already' } }),
      ),
    )
    const { wrapper, router } = await open(sam)
    await button(wrapper, 'Join')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('reports a 401 unauthorized on accept', async () => {
    usePreview()
    server.use(
      http.post('*/v1/invites/tok/accept', () =>
        json(401, { error: { code: 'unauthorized', message: 'expired' } }),
      ),
    )
    const fn = vi.fn()
    onUnauthorized(fn)
    const { wrapper } = await open(sam)
    await button(wrapper, 'Join')!.trigger('click')
    await flushPromises()
    await flushPromises()
    expect(fn).toHaveBeenCalledOnce()
  })
})

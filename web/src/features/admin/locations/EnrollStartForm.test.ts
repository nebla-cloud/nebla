import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp } from '@/test/mount'
import EnrollStartForm from './EnrollStartForm.vue'

beforeEach(() => {
  document.body.innerHTML = ''
})

const enrollment = (id: number, name: string) => ({
  id,
  name,
  description: '',
  location_id: null,
  token_name: 'nebla-x-abc123',
  state: 'waiting',
  expired: false,
  expires_at: '2026-01-01T00:30:00Z',
  created: '2026-01-01T00:00:00Z',
  report: null,
})

const mountForm = () => {
  const started: number[] = []
  let manual = 0
  const m = mountApp(EnrollStartForm, {
    props: { onStarted: (id: number) => started.push(id), onManual: () => manual++ },
  })
  return { ...m, started, manual: () => manual }
}

async function fill(wrapper: ReturnType<typeof mountApp>['wrapper'], name: string) {
  await wrapper.find('input[name=name]').setValue(name)
  await wrapper.find('input[name=description]').setValue('Rack 2')
  await wrapper.find('form').trigger('submit')
  await flushPromises()
  await new Promise((r) => setTimeout(r, 50))
  await flushPromises()
}

describe('EnrollStartForm', () => {
  it('rejects an invalid name', async () => {
    let posted = false
    server.use(
      http.post('*/v1/admin/location_enrollments', () => {
        posted = true
        return json(201, {})
      }),
    )
    const { wrapper } = mountForm()
    await fill(wrapper, 'Home!')
    expect(wrapper.text()).toContain('Use 1 to 32 lowercase letters, numbers or hyphens.')
    expect(posted).toBe(false)
  })

  it('posts the form and emits started with the id', async () => {
    let body: unknown
    server.use(
      http.post('*/v1/admin/location_enrollments', async ({ request }) => {
        body = await request.json()
        return json(201, {
          enrollment: enrollment(7, 'home'),
          token: 'nbe_x',
          command: 'curl',
          offline_script: 'sh',
        })
      }),
    )
    const { wrapper, started } = mountForm()
    await fill(wrapper, 'home')
    expect(body).toEqual({ name: 'home', description: 'Rack 2' })
    await vi.waitFor(() => expect(started).toEqual([7]))
  })

  it('shows the API message on a name clash', async () => {
    server.use(
      http.post('*/v1/admin/location_enrollments', () =>
        json(409, { error: { code: 'name_taken', message: 'That name is already used.' } }),
      ),
    )
    const { wrapper, started } = mountForm()
    await fill(wrapper, 'home')
    expect(wrapper.text()).toContain('That name is already used.')
    expect(started).toEqual([])
  })

  it('offers connecting manually', async () => {
    const { wrapper, manual } = mountForm()
    const b = wrapper.findAll('button').find((x) => x.text() === 'Connect manually')!
    await b.trigger('click')
    expect(manual()).toBe(1)
  })
})

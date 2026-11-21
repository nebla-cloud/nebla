import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { mountApp, testRouter } from '@/test/mount'
import type { Role } from '@/api/types'
import { makeServer } from '@/features/servers/fixtures'
import { applyAction, listMeta, makeFirewall } from './fixtures'
import type { Firewall } from './queries'
import FirewallDetailPage from './FirewallDetailPage.vue'

let fw: Firewall
let calls: { path: string; body: unknown }[]
let setRulesStatus: number
let mounted: VueWrapper | null = null

const servers = [makeServer({ id: 1, name: 'web1', labels: { role: 'web' } }), makeServer({ id: 2, name: 'db1' })]

function serve(role: Role = 'owner') {
  const record = (path: string) => async ({ request }: { request: Request }) => {
    calls.push({ path, body: await request.json() })
    if (path === 'set_rules' && setRulesStatus !== 201)
      return json(setRulesStatus, { error: { code: 'invalid_input', message: 'Port 22 is listed twice.' } })
    return json(201, { actions: [applyAction(11, 1)] })
  }
  server.use(
    http.get('*/v1/projects/7', () => json(200, { id: 7, name: 'Home', role })),
    http.get('*/v1/firewalls/3', () => json(200, fw)),
    http.get('*/v1/servers', ({ request }) => {
      const sel = new URL(request.url).searchParams.get('label_selector')
      const list = sel === 'role=web' ? servers.slice(0, 1) : sel ? [] : servers
      return json(200, { servers: list, meta: listMeta(list.length) })
    }),
    http.get('*/v1/actions/11', () => json(200, applyAction(11, 1))),
    http.post('*/v1/firewalls/3/actions/set_rules', record('set_rules')),
    http.post('*/v1/firewalls/3/actions/apply_to_resources', record('apply_to_resources')),
    http.post('*/v1/firewalls/3/actions/remove_from_resources', record('remove_from_resources')),
  )
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await new Promise((r) => setTimeout(r, 10))
    await flushPromises()
  }
}

async function mountPage() {
  const router = testRouter([
    { path: '/p/:projectId/firewalls', name: 'firewalls', component: FirewallDetailPage },
    { path: '/p/:projectId/firewalls/:firewallId', name: 'firewall', component: FirewallDetailPage },
    { path: '/p/:projectId/servers/:serverId', name: 'server', component: FirewallDetailPage },
  ])
  await router.push('/p/7/firewalls/3')
  const m = mountApp(FirewallDetailPage, { router })
  mounted = m.wrapper
  await settle()
  return m
}

const text = () => document.body.textContent ?? ''
const buttons = () => Array.from(document.body.querySelectorAll('button'))
const byText = (t: string) => buttons().find((b) => b.textContent?.trim() === t)
const byLabel = (t: string) => document.body.querySelector<HTMLButtonElement>(`button[aria-label="${t}"]`)
const $ = <T extends Element = HTMLInputElement>(sel: string) => document.body.querySelector(sel) as T

async function fill(sel: string, value: string) {
  const el = $<HTMLInputElement>(sel)
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

async function choose(sel: string, value: string) {
  const el = $<HTMLSelectElement>(sel)
  el.value = value
  el.dispatchEvent(new Event('change'))
  await flushPromises()
}

beforeEach(() => {
  calls = []
  setRulesStatus = 201
  fw = makeFirewall({
    rules: [{ direction: 'out', protocol: 'udp', port: '53', destination_ips: ['0.0.0.0/0'] }],
    applied_to: [{ type: 'server', server: { id: 2 } }],
  })
})

afterEach(() => {
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
})

describe('RuleEditor', () => {
  it('adds an inbound tcp 22 rule and saves the exact rules body', async () => {
    serve()
    await mountPage()
    expect(text()).toContain('Inbound')
    expect(text()).toContain('Outbound')
    byLabel('Add inbound rule')!.click()
    await flushPromises()
    expect($<HTMLSelectElement>('select[name="in-0-protocol"]').value).toBe('tcp')
    await fill('input[name="in-0-port"]', '22')
    byText('Save rules')!.click()
    await settle()
    expect(calls).toEqual([
      {
        path: 'set_rules',
        body: {
          rules: [
            { direction: 'in', protocol: 'tcp', port: '22', source_ips: ['0.0.0.0/0', '::/0'] },
            { direction: 'out', protocol: 'udp', port: '53', destination_ips: ['0.0.0.0/0'] },
          ],
        },
      },
    ])
    await vi.waitFor(() => expect(text()).toContain('Applying firewalls'))
  })

  it('rejects port 70000 inline without sending', async () => {
    serve()
    await mountPage()
    byLabel('Add inbound rule')!.click()
    await flushPromises()
    await fill('input[name="in-0-port"]', '70000')
    byText('Save rules')!.click()
    await settle()
    expect(calls).toEqual([])
    expect(text()).toContain('Use a port from 1 to 65535')
  })

  it('drops the port for icmp', async () => {
    serve()
    await mountPage()
    byLabel('Add inbound rule')!.click()
    await flushPromises()
    await choose('select[name="in-0-protocol"]', 'icmp')
    expect($('input[name="in-0-port"]')).toBeNull()
    byText('Save rules')!.click()
    await settle()
    expect((calls[0]!.body as { rules: unknown[] }).rules[0]).toEqual({
      direction: 'in',
      protocol: 'icmp',
      source_ips: ['0.0.0.0/0', '::/0'],
    })
  })

  it("shows the API's 422 message", async () => {
    setRulesStatus = 422
    serve()
    await mountPage()
    byLabel('Add inbound rule')!.click()
    await flushPromises()
    await fill('input[name="in-0-port"]', '22')
    byText('Save rules')!.click()
    await settle()
    expect(text()).toContain('Port 22 is listed twice.')
  })
})

describe('AppliedToPanel', () => {
  it('lists the servers it applies to', async () => {
    serve()
    await mountPage()
    expect(text()).toContain('Applied to')
    expect(text()).toContain('db1')
  })

  it('previews matched servers for role=web and posts apply_to_resources', async () => {
    serve()
    await mountPage()
    byText('Add')!.click()
    await flushPromises()
    $('input[name="target"][value="label_selector"]').click()
    await flushPromises()
    await fill('input[name="selector"]', 'role=web')
    await vi.waitFor(() => expect($('[data-preview]')?.textContent).toContain('web1'))
    expect($('[data-preview]').textContent).not.toContain('db1')
    $<HTMLFormElement>('#apply-firewall').requestSubmit()
    await settle()
    expect(calls).toEqual([
      {
        path: 'apply_to_resources',
        body: { apply_to: [{ type: 'label_selector', label_selector: { selector: 'role=web' } }] },
      },
    ])
  })

  it('applies to a single server', async () => {
    serve()
    await mountPage()
    byText('Add')!.click()
    await flushPromises()
    await choose('select[name="server"]', '1')
    $<HTMLFormElement>('#apply-firewall').requestSubmit()
    await settle()
    expect(calls).toEqual([
      { path: 'apply_to_resources', body: { apply_to: [{ type: 'server', server: { id: 1 } }] } },
    ])
  })

  it('removing posts remove_from_resources', async () => {
    serve()
    await mountPage()
    byLabel('Remove db1')!.click()
    await settle()
    expect(calls).toEqual([
      { path: 'remove_from_resources', body: { remove_from: [{ type: 'server', server: { id: 2 } }] } },
    ])
  })
})

describe('read-only role', () => {
  it('sees no add, edit or delete buttons', async () => {
    serve('read_only')
    await mountPage()
    expect(text()).toContain('udp')
    expect(text()).toContain('db1')
    for (const t of ['Add', 'Save rules', 'Rename', 'Delete']) expect(byText(t)).toBeUndefined()
    expect(byLabel('Add inbound rule')).toBeNull()
    expect(byLabel('Remove db1')).toBeNull()
    expect(document.body.querySelector('input, select')).toBeNull()
  })
})

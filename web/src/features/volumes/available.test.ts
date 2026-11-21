import { describe, expect, it } from 'vitest'
import { http } from 'msw'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { navItems } from '@/router/nav'
import { testQueryClient } from '@/test/mount'
import { json, server } from '@/test/server'
import { setProjectSource } from '@/api/client'
import { anyVolumes, trackAvailability, volumesNavHidden } from './available'
import { makeLocation } from './fixtures'
import { settle } from './testkit'

const Stub = defineComponent({ render: () => h('div') })

describe('volumes nav entry', () => {
  it('anyVolumes needs one location with volumes', () => {
    expect(anyVolumes(undefined)).toBeUndefined()
    expect(anyVolumes([makeLocation({ volumes_available: false })])).toBe(false)
    expect(anyVolumes([makeLocation({ volumes_available: false }), makeLocation({ id: 2 })])).toBe(
      true,
    )
  })

  async function setup(pid: number, available: boolean) {
    const qc = testQueryClient()
    trackAvailability(qc)
    server.use(
      http.get('*/v1/locations', () =>
        json(200, { locations: [makeLocation({ volumes_available: available })], meta: {} }),
      ),
    )
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/p/:projectId/volumes',
          name: 'volumes',
          component: Stub,
          meta: {
            side: 'project',
            nav: { label: 'Volumes', order: 12, hidden: (r) => volumesNavHidden(r, qc) },
          },
        },
      ],
    })
    setProjectSource(() => pid)
    await router.push(`/p/${pid}/volumes`)
    return router
  }

  it('shows once a location offers volumes', async () => {
    const router = await setup(41, true)
    expect(navItems(router, 'project').map((i) => i.label)).not.toContain('Volumes')
    await settle()
    expect(navItems(router, 'project').map((i) => i.label)).toContain('Volumes')
  })

  it('sits in the top group next to Servers', async () => {
    const { createAppRouter } = await import('@/router')
    const r = createAppRouter(createMemoryHistory())
      .getRoutes()
      .find((x) => x.name === 'volumes')!
    expect(r.meta.nav!.group).toBeUndefined()
    expect(r.meta.nav!.order).toBe(12)
  })

  it('forgets a project once its locations leave the cache', async () => {
    const qc = testQueryClient()
    trackAvailability(qc)
    qc.setQueryData(['locations', 43], [makeLocation()])
    const route = { params: { projectId: '43' } } as unknown as Parameters<
      typeof volumesNavHidden
    >[0]
    expect(volumesNavHidden(route, qc)).toBe(false)
    server.use(http.get('*/v1/locations', () => new Promise<Response>(() => {})))
    qc.clear()
    expect(volumesNavHidden(route, qc)).toBe(true)
  })

  it('stays hidden when no location offers volumes', async () => {
    const router = await setup(42, false)
    await settle()
    expect(navItems(router, 'project').map((i) => i.label)).not.toContain('Volumes')
  })
})

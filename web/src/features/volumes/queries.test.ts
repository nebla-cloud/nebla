import { describe, expect, it } from 'vitest'
import { http } from 'msw'
import { json, server } from '@/test/server'
import { makeVolume } from './fixtures'
import { fetchVolumes } from './queries'

describe('fetchVolumes', () => {
  it('reads every page', async () => {
    const all = Array.from({ length: 120 }, (_, i) => makeVolume({ id: i + 1, name: `v${i + 1}` }))
    const seen: string[] = []
    server.use(
      http.get('*/v1/volumes', ({ request }) => {
        const u = new URL(request.url)
        seen.push(u.search)
        const page = Number(u.searchParams.get('page'))
        const per = Number(u.searchParams.get('per_page'))
        return json(200, {
          volumes: all.slice((page - 1) * per, page * per),
          meta: { pagination: { page, per_page: per, total_entries: all.length } },
        })
      }),
    )
    const got = await fetchVolumes()
    expect(got.map((v) => v.id)).toEqual(all.map((v) => v.id))
    expect(seen).toHaveLength(3)
  })

  it('pages the volumes of one server', async () => {
    const servers: (string | null)[] = []
    server.use(
      http.get('*/v1/volumes', ({ request }) => {
        servers.push(new URL(request.url).searchParams.get('server'))
        return json(200, {
          volumes: [makeVolume({ id: 1 })],
          meta: { pagination: { page: 1, per_page: 50, total_entries: 1 } },
        })
      }),
    )
    expect(await fetchVolumes(5)).toHaveLength(1)
    expect(servers).toEqual(['5'])
  })
})

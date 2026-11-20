import type { RouteLocationNormalized, RouteLocationRaw, Router } from 'vue-router'
import { setProjectId } from '@/api/client'
import { ApiError } from '@/api/errors'
import type { User } from '@/api/types'
import { panelReady } from '@/features/admin/getready/checklist'
import { projectQuery, projectsQuery } from '@/features/projects/queries'
import { parseProjectId, readLastProject } from '@/lib/project'
import { queryClient } from '@/lib/queryClient'
import { ensureSession, useSession } from '@/lib/session'

export function safeNext(v: unknown): string | null {
  if (typeof v !== 'string' || !v.startsWith('/')) return null
  if (v.startsWith('//') || v.startsWith('/\\')) return null
  return v
}

// Renders the not-found page while keeping the address the user typed.
export function notFound(to: RouteLocationNormalized): RouteLocationRaw {
  return {
    name: 'not-found',
    params: { pathMatch: to.path.split('/').slice(1) },
    query: to.query,
    hash: to.hash,
  }
}

export async function resolveRoot(me: User): Promise<RouteLocationRaw> {
  if (me.is_admin && !(await panelReady(queryClient))) return { name: 'admin-get-ready' }
  const projects = await queryClient.fetchQuery(projectsQuery())
  const last = readLastProject()
  const pick = projects.find((p) => p.id === last) ?? projects[0]
  return pick ? { path: `/p/${pick.id}/servers` } : { name: 'projects' }
}

async function projectExists(id: number): Promise<boolean> {
  try {
    await queryClient.fetchQuery({ ...projectQuery(id), staleTime: 30_000 })
    return true
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 403)) return false
    throw e
  }
}

export function installGuards(router: Router): void {
  router.beforeEach(async (to) => {
    await ensureSession()
    const { manifest, me } = useSession()

    if (manifest.value?.setup_required) return to.name === 'setup' ? true : { name: 'setup' }
    if (to.name === 'setup') return { name: 'root' }

    if (to.meta.public) {
      if (to.name === 'login' && me.value) return safeNext(to.query.next) ?? { name: 'root' }
      return true
    }
    if (!me.value) return { name: 'login', query: { next: to.fullPath } }

    if (to.meta.side === 'admin' && !me.value.is_admin) return notFound(to)
    if (to.name === 'root') return resolveRoot(me.value)

    if ('projectId' in to.params) {
      const id = parseProjectId(to.params.projectId)
      if (id === null || !(await projectExists(id))) return notFound(to)
      setProjectId(id)
    }
    return true
  })
}

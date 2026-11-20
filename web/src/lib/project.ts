import { computed, watch, type ComputedRef, type Ref } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { setProjectId } from '@/api/client'
import type { Project } from '@/api/types'
import { projectQuery } from '@/features/projects/queries'
import { readStorage, writeStorage } from './storage'

const LAST_PROJECT = 'nebla.lastProject'

export function parseProjectId(v: unknown): number | null {
  if (typeof v !== 'string' || !/^[1-9]\d*$/.test(v)) return null
  const n = Number(v)
  return Number.isSafeInteger(n) ? n : null
}

export function readLastProject(): number | null {
  return parseProjectId(readStorage(LAST_PROJECT))
}

export function writeLastProject(id: number): void {
  writeStorage(LAST_PROJECT, String(id))
}

export function useCurrentProject(): {
  projectId: ComputedRef<number>
  project: Ref<Project | undefined>
} {
  const route = useRoute()
  const projectId = computed(() => parseProjectId(route.params.projectId) ?? 0)
  watch(
    projectId,
    (id) => {
      if (!id) return
      setProjectId(id)
      writeLastProject(id)
    },
    { immediate: true },
  )
  const { data: project } = useQuery(
    computed(() => ({ ...projectQuery(projectId.value), enabled: projectId.value > 0 })),
  )
  return { projectId, project }
}

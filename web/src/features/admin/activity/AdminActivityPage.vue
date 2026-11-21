<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import AdminPager from '@/components/AdminPager.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import ActionList from '@/features/activity/ActionList.vue'
import { useAdminProjects } from '@/features/admin/projects/queries'
import { selectClass } from '@/lib/ui'
import { usePageClamp } from '@/lib/usePageClamp'
import { PER_PAGE, useAdminActions, type AdminStatus } from './queries'

const route = useRoute()
const router = useRouter()

const statuses: AdminStatus[] = ['running', 'error', 'success']
const filters: { value: AdminStatus | null; label: string }[] = [
  { value: null, label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'error', label: 'Failed' },
  { value: 'success', label: 'Succeeded' },
]

const page = computed(() => Math.max(1, Number.parseInt(String(route.query.page ?? '1'), 10) || 1))
const projectId = computed(() => {
  const n = Number.parseInt(String(route.query.project ?? ''), 10)
  return n > 0 ? n : null
})
const status = computed(() => {
  const s = String(route.query.status ?? '') as AdminStatus
  return statuses.includes(s) ? s : null
})

const { data, isPending, error: loadError } = useAdminActions({ page, projectId, status })
const actions = computed(() => data.value?.actions ?? [])
const total = computed(() => data.value?.meta.pagination.total_entries ?? 0)
usePageClamp(
  page,
  computed(() => data.value?.actions.length),
  total,
  PER_PAGE,
)

const { data: projectsData } = useAdminProjects(ref(1))
const projectOptions = computed(() => {
  const list = (projectsData.value?.projects ?? []).map((p) => ({ id: p.id, name: p.name }))
  const id = projectId.value
  if (id && !list.some((p) => p.id === id)) {
    const known = actions.value.find((a) => a.project_id === id)
    list.push({ id, name: known?.project_name ?? `Project ${id}` })
  }
  return list
})

function setQuery(patch: LocationQueryRaw) {
  const query: LocationQueryRaw = { ...route.query, ...patch }
  for (const k of Object.keys(query)) if (query[k] == null || query[k] === '') delete query[k]
  void router.push({ query })
}

const setStatus = (s: AdminStatus | null) => setQuery({ status: s, page: null })
const setProject = (e: Event) =>
  setQuery({ project: (e.target as HTMLSelectElement).value || null, page: null })
const setPage = (p: number) => setQuery({ page: String(p) })
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="Activity" description="Every action across all projects, newest first." />

    <div class="flex flex-wrap items-center gap-3">
      <select
        :value="projectId ? String(projectId) : ''"
        aria-label="Filter by project"
        :class="selectClass"
        @change="setProject"
      >
        <option value="">All projects</option>
        <option v-for="p in projectOptions" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
      </select>
      <div class="flex flex-wrap gap-2" role="group" aria-label="Filter by status">
        <UButton
          v-for="f in filters"
          :key="f.label"
          :label="f.label"
          size="sm"
          :variant="status === f.value ? 'solid' : 'outline'"
          :aria-pressed="status === f.value"
          @click="setStatus(f.value)"
        />
      </div>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading activity…</p>
    <EmptyState
      v-else-if="!actions.length"
      title="No activity"
      :body="
        projectId || status ? 'Nothing matches these filters.' : 'Nothing has happened here yet.'
      "
    />
    <ActionList v-else :actions="actions" :show-resource="false" project-names admin />
    <AdminPager :page="page" :per-page="PER_PAGE" :total="total" @update:page="setPage" />
  </div>
</template>

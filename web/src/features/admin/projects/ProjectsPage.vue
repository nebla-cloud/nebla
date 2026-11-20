<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { errorMessage } from '@/api/errors'
import AdminPager from '@/components/AdminPager.vue'
import EmptyState from '@/components/EmptyState.vue'
import { formatDate } from '@/lib/format'
import { usePageClamp } from '@/lib/usePageClamp'
import { PER_PAGE, useAdminProjects, type AdminProject } from './queries'

const route = useRoute()
const router = useRouter()

const page = computed(() => Math.max(1, Number.parseInt(String(route.query.page ?? '1'), 10) || 1))
const { data, isPending, error: loadError } = useAdminProjects(page)
const projects = computed(() => data.value?.projects ?? [])
const total = computed(() => data.value?.meta.pagination.total_entries ?? 0)
usePageClamp(
  page,
  computed(() => data.value?.projects.length),
  total,
  PER_PAGE,
)

const servers = (p: AdminProject) =>
  p.quota.max_servers > 0 ? `${p.usage.servers} of ${p.quota.max_servers}` : `${p.usage.servers}`

function setPage(p: number) {
  void router.push({ query: { ...route.query, page: String(p) } })
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">Projects</h1>
      <p class="text-sm text-muted">Every project on this panel, with its limits.</p>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading projects…</p>
    <EmptyState
      v-else-if="!projects.length"
      title="No projects"
      body="Nobody has made a project yet."
    />
    <div v-else class="overflow-hidden rounded-lg border border-default">
      <table class="block w-full text-left text-sm sm:table">
        <thead class="hidden bg-elevated text-muted sm:table-header-group">
          <tr>
            <th class="px-4 py-2.5 font-medium">Name</th>
            <th class="px-4 py-2.5 font-medium">Owner email</th>
            <th class="px-4 py-2.5 font-medium">Servers</th>
            <th class="px-4 py-2.5 font-medium">Members</th>
            <th class="px-4 py-2.5 font-medium">Created</th>
          </tr>
        </thead>
        <tbody class="block divide-y divide-default sm:table-row-group">
          <tr
            v-for="p in projects"
            :key="p.id"
            class="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3.5 sm:table-row sm:px-0 sm:py-0"
          >
            <td class="min-w-0 font-semibold text-highlighted sm:table-cell sm:px-4 sm:py-3">
              <RouterLink
                :to="`/admin/projects/${p.id}`"
                class="underline decoration-accented underline-offset-4 hover:decoration-current"
              >
                {{ p.name }}
              </RouterLink>
            </td>
            <td class="order-last basis-full truncate text-muted sm:table-cell sm:px-4 sm:py-3">
              {{ p.owner_email }}
            </td>
            <td class="text-muted sm:table-cell sm:px-4 sm:py-3">
              <span class="sm:hidden">Servers </span>{{ servers(p) }}
              <span v-if="p.quota.max_servers === 0" class="text-muted">No limit</span>
            </td>
            <td class="text-muted sm:table-cell sm:px-4 sm:py-3">
              <span class="sm:hidden">Members </span>{{ p.member_count }}
            </td>
            <td class="text-muted sm:table-cell sm:px-4 sm:py-3">{{ formatDate(p.created) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <AdminPager :page="page" :per-page="PER_PAGE" :total="total" @update:page="setPage" />
  </div>
</template>

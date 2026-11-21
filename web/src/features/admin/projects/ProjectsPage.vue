<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { errorMessage } from '@/api/errors'
import AdminPager from '@/components/AdminPager.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate } from '@/lib/format'
import { table, tableWrap, tbody, td, th, thead, tr } from '@/lib/ui'
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
  <div class="flex flex-col gap-4">
    <PageHeader title="Projects" description="Every project on this panel, with its limits." />

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
    <div v-else :class="tableWrap">
      <table :class="table">
        <thead :class="thead">
          <tr>
            <th scope="col" :class="th">Name</th>
            <th scope="col" :class="th">Owner email</th>
            <th scope="col" :class="th">Servers</th>
            <th scope="col" :class="th">Members</th>
            <th scope="col" :class="th">Created</th>
          </tr>
        </thead>
        <tbody :class="tbody">
          <tr v-for="p in projects" :key="p.id" :class="tr">
            <td :class="[td, 'min-w-0 font-semibold text-highlighted']">
              <RouterLink
                :to="`/admin/projects/${p.id}`"
                class="underline decoration-accented underline-offset-4 hover:decoration-current"
              >
                {{ p.name }}
              </RouterLink>
            </td>
            <td :class="[td, 'order-last basis-full truncate text-muted']">
              {{ p.owner_email }}
            </td>
            <td :class="[td, 'text-muted']">
              <span class="sm:hidden">Servers </span>{{ servers(p) }}
              <span v-if="p.quota.max_servers === 0" class="text-muted">No limit</span>
            </td>
            <td :class="[td, 'text-muted']">
              <span class="sm:hidden">Members </span>{{ p.member_count }}
            </td>
            <td :class="[td, 'text-muted']">{{ formatDate(p.created) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <AdminPager :page="page" :per-page="PER_PAGE" :total="total" @update:page="setPage" />
  </div>
</template>

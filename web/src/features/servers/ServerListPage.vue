<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { ApiError, errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusArch from '@/components/StatusArch.vue'
import { useCurrentProject } from '@/lib/project'
import { serverStatus } from '@/lib/status'
import LabelChips from './LabelChips.vue'
import OsBadge from './OsBadge.vue'
import { osLabel } from './osLabel'
import { useServers } from './queries'
import { filterServers, parseStatusFilter, type StatusFilter } from './statusFilter'

const { projectId, project } = useCurrentProject()
// Shared by the head row and each server row so the columns line up.
const cols =
  'sm:grid sm:w-[30rem] sm:shrink-0 sm:grid-cols-[1.2fr_1.1fr_1.3fr_0.9fr] sm:gap-x-3 lg:w-[38rem] xl:w-[44rem] [&>*]:min-w-0 [&>*]:truncate'
const newRoute = computed(() => ({ name: 'server-new', params: { projectId: projectId.value } }))
const route = useRoute()
const router = useRouter()
const selector = computed(() => (typeof route.query.labels === 'string' ? route.query.labels : ''))
const filter = ref(selector.value)
const { data: servers, isPending, error } = useServers(selector)
const filterError = computed(() =>
  error.value instanceof ApiError && error.value.status === 422 ? error.value.message : null,
)
const listError = computed(() => (filterError.value ? null : error.value))

const status = computed(() => parseStatusFilter(route.query.status))
const visible = computed(() => filterServers(servers.value ?? [], status.value))
const counts = computed(() => {
  const list = servers.value ?? []
  return {
    all: list.length,
    running: filterServers(list, 'running').length,
    off: filterServers(list, 'off').length,
  }
})
const options: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'off', label: 'Off' },
]
const chipGroup = useTemplateRef<HTMLElement>('chipGroup')
function setStatus(v: StatusFilter) {
  void router.replace({ query: { ...route.query, status: v === 'all' ? undefined : v } })
}
function onChipKey(e: KeyboardEvent, i: number) {
  const step =
    e.key === 'ArrowRight' || e.key === 'ArrowDown'
      ? 1
      : e.key === 'ArrowLeft' || e.key === 'ArrowUp'
        ? -1
        : 0
  if (!step) return
  e.preventDefault()
  const n = (i + step + options.length) % options.length
  setStatus(options[n]!.value)
  const radios = chipGroup.value?.querySelectorAll<HTMLElement>('[role="radio"]')
  void nextTick(() => radios?.[n]?.focus())
}
function clearFilters() {
  clearTimeout(timer)
  filter.value = ''
  void router.replace({ query: { ...route.query, labels: undefined, status: undefined } })
}

const description = computed(() => {
  const list = servers.value
  if (!list?.length || !project.value || selector.value || status.value !== 'all') return undefined
  const running = list.filter((s) => s.status === 'running').length
  const n = list.length
  return `${n} ${n === 1 ? 'server' : 'servers'} in ${project.value.name}, ${running} running`
})

let timer: ReturnType<typeof setTimeout> | undefined
function commit() {
  clearTimeout(timer)
  const v = filter.value.trim()
  if (v === selector.value) return
  void router.replace({ query: { ...route.query, labels: v || undefined } })
}
function clear() {
  filter.value = ''
  commit()
}
watch(filter, () => {
  clearTimeout(timer)
  timer = setTimeout(commit, 400)
})
watch(selector, (v) => {
  if (v !== filter.value.trim()) filter.value = v
})
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="Servers" :description="description">
      <template v-if="servers?.length" #actions>
        <UButton :to="newRoute">Create server</UButton>
      </template>
    </PageHeader>

    <form
      v-if="servers?.length || selector || filter"
      role="search"
      novalidate
      @submit.prevent="commit"
    >
      <UFormField name="labels" label="Filter by label" :error="filterError ?? undefined">
        <UInput
          v-model="filter"
          name="labels"
          placeholder="env=prod,team"
          autocomplete="off"
          size="sm"
          class="w-full sm:max-w-sm"
          :ui="{ trailing: 'pe-1' }"
        >
          <template v-if="filter" #trailing>
            <UButton
              color="neutral"
              variant="link"
              size="sm"
              icon="i-lucide-circle-x"
              aria-label="Clear filter"
              @click="clear"
            />
          </template>
        </UInput>
      </UFormField>
    </form>

    <div
      v-if="!isPending && (servers?.length || status !== 'all' || selector)"
      ref="chipGroup"
      role="radiogroup"
      aria-label="Filter by status"
      class="flex flex-wrap items-center gap-2"
    >
      <button
        v-for="(o, i) in options"
        :key="o.value"
        type="button"
        role="radio"
        :aria-checked="status === o.value"
        :tabindex="status === o.value ? 0 : -1"
        class="inline-flex h-[30px] cursor-pointer items-center gap-1.5 rounded-full border px-3 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--nb-focus)"
        :class="
          status === o.value
            ? 'border-inverted bg-inverted text-inverted'
            : 'border-default text-default hover:bg-elevated'
        "
        @click="setStatus(o.value)"
        @keydown="onChipKey($event, i)"
      >
        {{ o.label }}
        <span class="text-xs" :class="status === o.value ? 'opacity-70' : 'text-muted'">{{
          counts[o.value]
        }}</span>
      </button>
    </div>

    <UAlert
      v-if="listError"
      color="error"
      variant="subtle"
      :description="errorMessage(listError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading servers…</p>
    <p v-else-if="filterError" class="text-muted">Fix the filter to see servers.</p>
    <div
      v-else-if="servers?.length && !visible.length"
      class="flex flex-wrap items-center gap-3 text-muted"
    >
      <p>No servers match this filter.</p>
      <UButton color="neutral" variant="outline" size="sm" @click="clearFilters"
        >Clear filters</UButton
      >
    </div>
    <p v-else-if="!servers?.length && selector" class="text-muted">No servers match this filter.</p>
    <EmptyState
      v-else-if="!servers?.length"
      title="No servers yet"
      body="Create a server to get started. It is ready in about a minute."
    >
      <UButton :to="newRoute">Create server</UButton>
    </EmptyState>
    <ListGroup v-else>
      <template #head>
        <div
          aria-hidden="true"
          class="hidden items-center gap-3 border-b border-default bg-(--nb-soft) px-4 py-2.5 text-[12.5px] font-medium text-muted sm:flex"
        >
          <span class="min-w-0 flex-1">Name</span>
          <span :class="cols">
            <span>Image</span>
            <span>Type</span>
            <span>Public IPv4</span>
            <span>Location</span>
          </span>
        </div>
      </template>
      <ListRow
        v-for="s in visible"
        :key="s.id"
        :to="{ name: 'server', params: { projectId, serverId: s.id } }"
      >
        <template #leading><StatusArch :status="s.status" /></template>
        <span
          class="flex min-w-0 flex-col gap-1 whitespace-normal sm:flex-row sm:items-center sm:gap-3"
        >
          <span class="flex min-w-0 flex-1 items-center gap-x-2">
            <span class="truncate">{{ s.name }}</span>
            <LabelChips :labels="s.labels" :max="3" />
          </span>
          <span :class="[cols, 'flex flex-wrap gap-x-4 text-sm font-normal text-default']">
            <span class="flex items-center gap-2" :title="s.image.name">
              <OsBadge :name="s.image.os_flavor" />
              <span class="truncate">{{ osLabel(s.image) }}</span>
            </span>
            <span
              >{{ s.server_type.name }}
              <span class="text-muted">· {{ s.server_type.cores }} vCPU</span></span
            >
            <span v-if="s.status === 'running'" class="nb-mono">{{
              s.public_net.ipv4?.ip ?? '—'
            }}</span>
            <span v-else class="text-muted">{{ serverStatus(s.status).label }}</span>
            <span>{{ s.location.name }}</span>
          </span>
        </span>
      </ListRow>
    </ListGroup>
  </div>
</template>

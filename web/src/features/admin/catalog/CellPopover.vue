<script setup lang="ts">
import { computed } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { formatDate } from '@/lib/format'
import { canDisable, cellActions, cellView, type CellPrimary } from './cellState'
import type { CatalogEntry, CatalogLocationState, CatalogNode } from './queries'

const props = defineProps<{
  entry: CatalogEntry
  loc: CatalogLocationState
  locationName: string
  busy?: boolean
}>()
const emit = defineEmits<{ run: [action: CellPrimary]; disable: [] }>()

const view = computed(() => cellView(props.loc, props.entry.retired))
const actions = computed(() => cellActions(props.loc, props.entry.retired))
const labels: Record<CellPrimary, string> = { enable: 'Enable', retry: 'Retry', refresh: 'Refresh' }
const statusLabels: Record<string, string> = {
  ready: 'Ready',
  building: 'Building',
  failed: 'Failed',
}

function nodeLine(n: CatalogNode): string {
  const status = statusLabels[n.status] ?? n.status
  if (n.status === 'ready' && n.built_at) return `${status}, built ${formatDate(n.built_at)}`
  return status
}
</script>

<template>
  <div class="flex w-72 max-w-[calc(100vw-2rem)] flex-col gap-3 p-3 text-sm">
    <div class="flex flex-col gap-1">
      <p class="font-semibold text-highlighted">{{ entry.description }} in {{ locationName }}</p>
      <p v-if="view.detail" class="text-muted">{{ view.detail }}</p>
    </div>

    <ul v-if="loc.nodes.length" class="flex flex-col gap-1.5">
      <li v-for="(n, i) in loc.nodes" :key="`${n.node}-${i}`" class="flex flex-col">
        <span>
          <span class="font-medium text-highlighted">{{ n.node || 'Shared storage' }}</span
          ><span :class="n.status === 'failed' ? 'text-error' : 'text-muted'"
            >: {{ nodeLine(n) }}</span
          >
        </span>
        <span v-if="n.error" class="break-words text-error">{{ n.error }}</span>
      </li>
    </ul>

    <div v-if="actions.length || canDisable(loc)" class="flex flex-wrap items-center gap-2">
      <UButton
        v-for="a in actions"
        :key="a"
        size="xs"
        color="neutral"
        variant="outline"
        :disabled="busy"
        @click="emit('run', a)"
      >
        {{ labels[a] }}
      </UButton>
      <UButton
        v-if="canDisable(loc)"
        size="xs"
        color="error"
        variant="ghost"
        class="ms-auto"
        :disabled="busy"
        @click="emit('disable')"
      >
        Disable
      </UButton>
    </div>
  </div>
</template>

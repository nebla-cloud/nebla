<script setup lang="ts">
import { computed, ref } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UPopover from '@nuxt/ui/components/Popover.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { useQueryClient } from '@tanstack/vue-query'
import { ApiError, errorMessage } from '@/api/errors'
import { cellView, primaryAction, type CellPrimary } from './cellState'
import CellPopover from './CellPopover.vue'
import {
  catalogKey,
  useCatalogAction,
  type CatalogAction,
  type CatalogEntry,
  type CatalogLocationState,
} from './queries'

const props = defineProps<{
  entry: CatalogEntry
  loc: CatalogLocationState
  locationName: string
}>()

const qc = useQueryClient()
const toast = useToast()
const mutation = useCatalogAction()
const view = computed(() => cellView(props.loc, props.entry.retired))
const open = ref(false)
const confirming = ref(false)
const labels: Record<CellPrimary, string> = { enable: 'Enable', retry: 'Retry', refresh: 'Refresh' }
const verbs: Record<CatalogAction, string> = {
  enable: 'enable',
  refresh: 'refresh',
  disable: 'disable',
}
const dot = {
  success: 'bg-(--nb-run)',
  neutral: 'bg-(--nb-off)',
  busy: 'bg-(--nb-busy)',
  error: 'bg-(--nb-err)',
  muted: 'border border-(--nb-off)',
}

async function run(action: CatalogAction) {
  try {
    await mutation.mutateAsync({ key: props.entry.key, locationId: props.loc.location_id, action })
    return true
  } catch (e) {
    // A 409 is silent only when the settle refetch shows a build already running.
    if (e instanceof ApiError && e.status === 409 && action !== 'disable' && nowBuilding()) {
      return false
    }
    toast.add({
      title: `Couldn't ${verbs[action]} ${props.entry.description} in ${props.locationName}`,
      description: errorMessage(e),
      color: 'error',
    })
    return false
  }
}

function nowBuilding(): boolean {
  const entries = qc.getQueryData<CatalogEntry[]>(catalogKey)
  const e = entries?.find((x) => x.key === props.entry.key)
  return e?.locations.find((l) => l.location_id === props.loc.location_id)?.state === 'building'
}

function runPrimary(p: CellPrimary) {
  open.value = false
  void run(primaryAction(p))
}

function askDisable() {
  open.value = false
  confirming.value = true
}

async function disable() {
  if (await run('disable')) confirming.value = false
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
    <UPopover v-model:open="open" :content="{ align: 'start' }">
      <button
        type="button"
        data-cell-trigger
        class="inline-flex items-center gap-1.5 rounded-[4px] text-start font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        :class="view.tone === 'muted' ? 'text-muted' : 'text-highlighted'"
        :aria-label="`${entry.description} in ${locationName}: ${view.label}. Show details`"
      >
        <span
          v-if="loc.state === 'partial'"
          aria-hidden="true"
          class="inline-block size-2 shrink-0 rounded-full bg-[linear-gradient(90deg,var(--nb-run)_50%,var(--nb-err)_50%)]"
        />
        <span
          v-else
          aria-hidden="true"
          class="inline-block size-2 shrink-0 rounded-full"
          :class="[dot[view.pulse ? 'busy' : view.tone], view.pulse && 'motion-safe:animate-pulse']"
        />
        <span class="underline decoration-dotted decoration-1 underline-offset-4">{{
          view.label
        }}</span>
      </button>
      <template #content>
        <CellPopover
          :entry="entry"
          :loc="loc"
          :location-name="locationName"
          :busy="mutation.isPending.value"
          @run="runPrimary"
          @disable="askDisable"
        />
      </template>
    </UPopover>
    <UButton
      v-if="view.primary"
      size="xs"
      color="neutral"
      variant="link"
      class="px-0 underline underline-offset-4"
      :loading="mutation.isPending.value"
      :aria-label="`${labels[view.primary]} ${entry.description} in ${locationName}`"
      @click="runPrimary(view.primary)"
    >
      {{ labels[view.primary] }}
    </UButton>

    <UModal
      v-model:open="confirming"
      :title="`Disable ${entry.description} in ${locationName}?`"
      description="People can't pick it for new servers here anymore. Nebla removes its templates from this location. You can enable it again later."
    >
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="confirming = false">Cancel</UButton>
          <UButton color="error" :loading="mutation.isPending.value" @click="disable">
            Disable
          </UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>

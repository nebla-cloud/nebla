<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { components } from '@/api/schema'
import { useSetQuota, type Quota } from './queries'

type Usage = components['schemas']['ProjectUsage']

const props = defineProps<{ projectId: number; quota: Quota; usage: Usage }>()

const toast = useToast()
const save = useSetQuota(computed(() => props.projectId))

interface Row {
  key: keyof Quota
  label: string
  unit: string
  used: () => number
  gb?: boolean
}

const rows: Row[] = [
  { key: 'max_servers', label: 'Servers', unit: '', used: () => props.usage.servers },
  { key: 'max_vcpu', label: 'vCPU', unit: '', used: () => props.usage.vcpu },
  {
    key: 'max_memory_mb',
    label: 'Memory',
    unit: ' GB',
    used: () => props.usage.memory_mb / 1024,
    gb: true,
  },
  { key: 'max_disk_gb', label: 'Disk', unit: ' GB', used: () => props.usage.disk_gb },
  { key: 'max_ips', label: 'IPs', unit: '', used: () => props.usage.ips },
  { key: 'max_volumes', label: 'Volumes', unit: '', used: () => props.usage.volumes },
  { key: 'max_volume_gb', label: 'Volume storage', unit: ' GB', used: () => props.usage.volume_gb },
]
const dataKey: Record<keyof Quota, string> = {
  max_servers: 'servers',
  max_vcpu: 'vcpu',
  max_memory_mb: 'memory_mb',
  max_disk_gb: 'disk_gb',
  max_ips: 'ips',
  max_volumes: 'volumes',
  max_volume_gb: 'volume_gb',
}

const num = (n: number) => String(Number(n.toFixed(2)))
const toField = (r: Row, q: Quota) => {
  const v = q[r.key]
  if (v <= 0) return ''
  return r.gb ? num(v / 1024) : String(v)
}
const limits = ref<Record<string, string>>({})
const err = ref<string | null>(null)

watch(
  () => JSON.stringify(props.quota),
  () => {
    limits.value = Object.fromEntries(rows.map((r) => [r.key, toField(r, props.quota)]))
  },
  { immediate: true },
)

function limitOf(r: Row): number {
  const n = Number.parseFloat(limits.value[r.key] ?? '')
  if (!Number.isFinite(n) || n <= 0) return 0
  return r.gb ? Math.round(n * 1024) : Math.round(n)
}

const below = (r: Row) => {
  const limit = limitOf(r)
  return limit > 0 && (r.gb ? limit < props.usage.memory_mb : limit < r.used())
}

const usedText = (r: Row) => `${num(r.used())}${r.unit}`

async function submit() {
  err.value = null
  const body = Object.fromEntries(rows.map((r) => [r.key, limitOf(r)])) as unknown as Quota
  try {
    await save.mutateAsync(body)
    toast.add({ title: 'Limits saved', color: 'success' })
  } catch (e) {
    err.value = errorMessage(e)
  }
}
</script>

<template>
  <form class="flex flex-col gap-4" @submit.prevent="submit">
    <p class="text-sm text-muted">Leave a limit empty, or set it to 0, for no limit.</p>
    <ul class="divide-y divide-default overflow-hidden rounded-lg border border-default">
      <li
        v-for="r in rows"
        :key="r.key"
        :data-row="dataKey[r.key]"
        class="flex flex-col gap-1.5 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
      >
        <div class="flex items-baseline justify-between gap-3 sm:block">
          <span class="font-semibold text-highlighted">{{ r.label }}</span>
          <span class="text-sm text-muted sm:ml-3">Used {{ usedText(r) }}</span>
        </div>
        <div class="flex flex-col gap-1 sm:items-end">
          <UInput
            v-model="limits[r.key]"
            :name="r.key"
            type="number"
            min="0"
            :step="r.gb ? 'any' : '1'"
            inputmode="decimal"
            placeholder="No limit"
            :aria-label="`${r.label} limit${r.unit}`"
            class="w-full sm:w-40"
          />
          <p v-if="below(r)" class="flex items-center gap-1.5 text-sm text-default">
            <UIcon name="i-lucide-triangle-alert" class="size-4 shrink-0" />
            Below current use. Nothing is removed, but new servers will be refused.
          </p>
        </div>
      </li>
    </ul>
    <p v-if="err" class="text-sm text-error" role="alert">{{ err }}</p>
    <div>
      <UButton type="submit" :loading="save.isPending.value">Save limits</UButton>
    </div>
  </form>
</template>

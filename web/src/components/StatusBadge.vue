<script setup lang="ts">
import { computed } from 'vue'
import type { Server } from '@/api/types'
import { serverStatus, type StatusTone } from '@/lib/status'
import StatusArch from './StatusArch.vue'

const props = defineProps<{ status: Server['status'] }>()
const s = computed(() => serverStatus(props.status))
const tone: Record<StatusTone, string> = {
  success: 'bg-[var(--nb-run-bg)] text-[var(--nb-run-fg)]',
  busy: 'bg-[var(--nb-soft)] text-[var(--nb-busy-fg)]',
  neutral: 'bg-[var(--nb-soft)] text-muted',
  error: 'bg-[var(--nb-soft)] text-muted',
}
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 rounded-t-full rounded-b-md px-2.5 py-1 text-xs font-semibold"
    :class="tone[s.tone]"
  >
    <StatusArch :status="status" :size="16" />
    {{ s.label }}
  </span>
</template>

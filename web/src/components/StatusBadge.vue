<script setup lang="ts">
import { computed } from 'vue'
import type { Server } from '@/api/types'
import { serverStatus, type StatusTone } from '@/lib/status'
import StatusArch from './StatusArch.vue'

const props = defineProps<{ status: Server['status'] }>()
const s = computed(() => serverStatus(props.status))
const tone: Record<StatusTone, string> = {
  success: 'border-transparent bg-[var(--nb-run-bg)] text-[var(--nb-run-fg)]',
  busy: 'border-transparent bg-[var(--nb-soft)] text-[var(--nb-busy-fg)]',
  neutral: 'border-(--ui-border) bg-(--nb-soft) text-toned',
  error: 'border-transparent bg-error/10 text-error',
}
</script>

<template>
  <span
    class="inline-flex h-7 items-center gap-[7px] rounded-t-[14px] rounded-b-[6px] border pr-3 pl-2 text-[13px] font-semibold"
    :class="tone[s.tone]"
  >
    <StatusArch :status="status" :size="16" decorative />
    {{ s.label }}
  </span>
</template>

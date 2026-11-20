<script setup lang="ts">
import { computed } from 'vue'
import type { Server } from '@/api/types'
import { serverStatus, type StatusTone } from '@/lib/status'

const props = defineProps<{ status: Server['status'] }>()
const s = computed(() => serverStatus(props.status))
const tone: Record<StatusTone, string> = {
  success: 'bg-[var(--nb-run)]',
  busy: 'bg-[var(--nb-busy)]',
  neutral: 'bg-[var(--nb-off)]',
  error: 'bg-[var(--nb-err)]',
}
</script>

<template>
  <span
    role="img"
    :aria-label="s.label"
    :title="s.label"
    class="inline-block size-2 shrink-0 rounded-full"
    :class="[tone[s.tone], s.pulse && 'motion-safe:animate-pulse']"
  />
</template>

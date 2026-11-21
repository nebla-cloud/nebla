<script setup lang="ts">
import { computed } from 'vue'
import type { Server, Volume } from '@/api/types'
import { serverStatus, volumeStatus, type StatusTone } from '@/lib/status'

const props = defineProps<{
  status: Server['status'] | Volume['status']
  kind?: 'server' | 'volume'
}>()
const s = computed(() =>
  props.kind === 'volume'
    ? volumeStatus(props.status as Volume['status'])
    : serverStatus(props.status as Server['status']),
)
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

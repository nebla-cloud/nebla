<script setup lang="ts">
import { computed } from 'vue'
import type { Server } from '@/api/types'
import { serverStatus, type StatusTone } from '@/lib/status'

const props = withDefaults(defineProps<{ status: Server['status']; size?: number }>(), {
  size: 22,
})
const s = computed(() => serverStatus(props.status))
const stroke: Record<StatusTone, string> = {
  success: 'var(--nb-run)',
  busy: 'var(--nb-busy)',
  neutral: 'var(--nb-off)',
  error: 'var(--nb-err)',
}
</script>

<template>
  <svg
    role="img"
    :aria-label="s.label"
    viewBox="0 0 64 64"
    :width="size"
    :height="size"
    fill="none"
    class="shrink-0"
  >
    <title>{{ s.label }}</title>
    <path
      d="M16 52V31a16 16 0 0 1 32 0v21"
      :stroke="stroke[s.tone]"
      stroke-width="10"
      stroke-linecap="round"
      stroke-linejoin="round"
      :stroke-dasharray="s.tone === 'busy' ? '7 11' : undefined"
      :class="s.tone === 'busy' && 'motion-safe:animate-nb-dash'"
    />
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UProgress from '@nuxt/ui/components/Progress.vue'
import type { Action } from '@/api/types'
import { actionLabel } from './actionLabel'

const props = defineProps<{ action: Action }>()

const label = computed(() => actionLabel(props.action.command))
const started = computed(() =>
  new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' }).format(
    new Date(props.action.started),
  ),
)
const failed = computed(() => props.action.status === 'error')
</script>

<template>
  <div class="flex flex-col gap-2.5" role="status" aria-live="polite">
    <div class="flex items-baseline justify-between gap-3 text-sm">
      <span class="font-semibold text-highlighted">{{ label }}</span>
      <span class="text-muted">Started at {{ started }}</span>
    </div>
    <UProgress
      :model-value="action.progress"
      :max="100"
      size="sm"
      :color="failed ? 'error' : 'primary'"
      :aria-label="`${label}, ${action.progress}%`"
    />
    <p v-if="failed && action.error" class="text-sm text-error">{{ action.error.message }}</p>
  </div>
</template>

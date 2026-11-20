<script setup lang="ts">
import { computed } from 'vue'
import { formatLabel } from './labels'

const props = withDefaults(defineProps<{ labels: Record<string, string>; max?: number }>(), {
  max: 3,
})
const entries = computed(() => Object.entries(props.labels).sort(([a], [b]) => a.localeCompare(b)))
const shown = computed(() => entries.value.slice(0, props.max))
const extra = computed(() => entries.value.length - shown.value.length)
</script>

<template>
  <ul v-if="entries.length" class="flex flex-wrap items-center gap-1.5" aria-label="Labels">
    <li
      v-for="[k, v] in shown"
      :key="k"
      class="max-w-40 truncate rounded-md bg-elevated px-1.5 py-0.5 text-xs text-default ring ring-default"
    >
      {{ formatLabel(k, v) }}
    </li>
    <li v-if="extra > 0" class="text-xs text-muted">+{{ extra }}</li>
  </ul>
</template>

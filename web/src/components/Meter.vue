<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ value: number; max: number; label: string }>()
const pct = computed(() =>
  props.max > 0 ? Math.min(100, Math.max(0, (props.value / props.max) * 100)) : 0,
)
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <div
      role="meter"
      :aria-valuenow="value"
      aria-valuemin="0"
      :aria-valuemax="max"
      :aria-label="label"
      class="h-1.5 overflow-hidden rounded-full bg-muted"
    >
      <div data-fill class="h-full rounded-full" :style="{ width: `${pct}%`, background: 'var(--nb-gradient)' }" />
    </div>
    <span v-if="$slots.default" class="text-sm text-muted tabular-nums"><slot /></span>
  </div>
</template>

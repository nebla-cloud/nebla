<script setup lang="ts">
defineProps<{ label: string; value: string | number; unit?: string }>()
</script>

<template>
  <div class="flex flex-col gap-2 rounded-(--nb-radius-panel) border border-default bg-default p-4">
    <span class="text-sm text-muted">{{ label }}</span>
    <span class="text-highlighted">
      <span data-value class="text-2xl font-bold tabular-nums">{{ value }}</span>
      <span v-if="unit" class="ml-1 text-sm text-muted">{{ unit }}</span>
    </span>
    <slot name="chart" />
  </div>
</template>

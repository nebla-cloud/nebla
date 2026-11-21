<script setup lang="ts">
defineProps<{ label?: string }>()
// A head slot (column titles) renders above the list, inside the same frame.
const frame = 'overflow-hidden rounded-(--nb-radius-panel) border border-default bg-elevated'
</script>

<template>
  <div v-if="$slots.head" :class="frame">
    <slot name="head" />
    <ul :aria-label="label" class="divide-y divide-default">
      <slot />
    </ul>
  </div>
  <ul v-else :aria-label="label" :class="['divide-y divide-default', frame]">
    <slot />
  </ul>
</template>

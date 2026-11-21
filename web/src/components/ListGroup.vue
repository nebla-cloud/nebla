<script setup lang="ts">
defineProps<{ label?: string }>()
</script>

<template>
  <ul
    :aria-label="label"
    class="divide-y divide-default overflow-hidden rounded-(--nb-radius-panel) border border-default bg-elevated"
  >
    <slot />
  </ul>
</template>

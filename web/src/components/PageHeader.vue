<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <header class="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
    <div class="flex min-w-0 flex-1 flex-col gap-1">
      <div class="flex min-w-0 items-center gap-3">
        <h1 class="nb-h1 min-w-0 truncate text-highlighted">{{ title }}</h1>
        <slot name="meta" />
      </div>
      <p v-if="description" class="text-muted">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{ title: string; body: string }>()
</script>

<template>
  <section
    class="flex flex-col items-start gap-4 rounded-lg border border-dashed border-accented px-6 py-10 sm:px-10"
  >
    <div class="flex max-w-prose flex-col gap-1.5">
      <h2 class="text-lg font-bold tracking-tight text-highlighted">{{ title }}</h2>
      <p class="text-muted">{{ body }}</p>
    </div>
    <div v-if="$slots.default" class="w-full">
      <slot />
    </div>
  </section>
</template>

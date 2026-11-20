<script setup lang="ts">
import Lockup from '@/components/Lockup.vue'

defineProps<{ title: string; description?: string }>()
</script>

<template>
  <main
    class="flex overflow-x-clip min-h-dvh flex-col items-center px-4 pt-[12vh] pb-12"
  >
    <Lockup :height="28" class="mb-8" />
    <div
      class="w-full max-w-sm rounded-[var(--nb-radius-panel)] border border-default bg-elevated p-6 sm:p-8"
    >
      <header class="mb-6 flex flex-col gap-1.5">
        <h1 class="nb-h1 !text-[1.5rem] text-highlighted">{{ title }}</h1>
        <p v-if="description" class="text-sm text-muted">{{ description }}</p>
      </header>
      <slot />
    </div>
    <div v-if="$slots.after" class="mt-6 text-sm text-muted">
      <slot name="after" />
    </div>
  </main>
</template>

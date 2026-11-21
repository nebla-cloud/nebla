<script setup lang="ts">
defineProps<{ title: string; body: string }>()
</script>

<template>
  <section
    class="grid items-center gap-8 py-8 lg:gap-10 lg:py-12"
    :class="$slots.aside ? 'lg:grid-cols-2' : ''"
  >
    <div class="flex flex-col items-start gap-4">
      <div class="flex max-w-prose flex-col gap-2">
        <h2 class="text-[1.375rem] font-extrabold tracking-[-0.035em] text-highlighted lg:text-[1.75rem]">
          {{ title }}
        </h2>
        <p class="text-muted">{{ body }}</p>
      </div>
      <div v-if="$slots.default" class="w-full">
        <slot />
      </div>
    </div>
    <div v-if="$slots.aside" class="min-w-0">
      <slot name="aside" />
    </div>
  </section>
</template>

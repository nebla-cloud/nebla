<script setup lang="ts">
import UIcon from '@nuxt/ui/components/Icon.vue'

defineProps<{ n: number; state: 'done' | 'current' | 'todo'; title: string }>()
</script>

<template>
  <li
    :data-step="n"
    :data-state="state"
    class="relative grid grid-cols-[1.75rem_1fr] gap-3.5 pb-4 not-last:before:absolute not-last:before:top-8 not-last:before:-bottom-1 not-last:before:left-[0.8125rem] not-last:before:w-[1.5px] not-last:before:bg-(--ui-border) not-last:before:content-['']"
  >
    <span
      class="flex size-7 items-center justify-center rounded-full border-[1.5px] text-[13px] font-semibold"
      :class="
        state === 'done'
          ? 'border-(--nb-focus) bg-(--nb-focus) text-(--ui-bg)'
          : state === 'current'
            ? 'border-(--nb-focus) text-highlighted'
            : 'border-(--ui-border) text-muted'
      "
    >
      <UIcon v-if="state === 'done'" name="i-lucide-check" class="size-4" aria-label="Done" />
      <template v-else>{{ n }}</template>
    </span>
    <div class="min-w-0">
      <h3 class="mt-1 mb-2.5 text-base font-semibold text-highlighted">{{ title }}</h3>
      <slot />
    </div>
  </li>
</template>

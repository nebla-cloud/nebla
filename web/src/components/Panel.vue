<script setup lang="ts">
withDefaults(defineProps<{ title?: string; as?: 'section' | 'div' }>(), { as: 'section' })
</script>

<template>
  <component
    :is="as"
    class="rounded-(--nb-radius-panel) border border-default bg-elevated p-[1.125rem]"
  >
    <div v-if="title || $slots['header-actions']" class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h2 v-if="title" class="nb-h2 min-w-0 text-highlighted">{{ title }}</h2>
      <div v-if="$slots['header-actions']" class="flex items-center gap-2">
        <slot name="header-actions" />
      </div>
    </div>
    <slot />
  </component>
</template>

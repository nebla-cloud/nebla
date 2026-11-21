<script setup lang="ts">
withDefaults(defineProps<{ caption?: string; as?: 'div' | 'pre' }>(), { as: 'div' })
</script>

<template>
  <div class="overflow-hidden rounded-[12px] border border-(--ui-border) bg-(--nb-term-bg) text-(--nb-term-fg)">
    <div v-if="caption" class="border-b border-(--nb-term-border) px-4 py-2 text-xs text-(--nb-term-muted)">
      {{ caption }}
    </div>
    <component :is="as" class="nb-mono m-0 px-4 py-3.5 text-[12.5px] leading-[1.7] wrap-anywhere whitespace-pre-wrap">
      <slot />
    </component>
  </div>
</template>

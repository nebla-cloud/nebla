<script setup lang="ts">
import { RouterLink, type RouteLocationRaw } from 'vue-router'

defineProps<{ to?: RouteLocationRaw }>()

const inner = 'flex items-center gap-3 px-4 py-3'
</script>

<template>
  <li>
    <RouterLink
      v-if="to"
      :to="to"
      :class="[
        inner,
        'hover:bg-(--nb-soft) focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary',
      ]"
    >
      <span v-if="$slots.leading" class="shrink-0"><slot name="leading" /></span>
      <span class="min-w-0 flex-1 truncate font-semibold text-highlighted"><slot /></span>
      <span v-if="$slots.meta" class="shrink-0 text-sm text-muted"><slot name="meta" /></span>
      <span v-if="$slots.trailing" class="shrink-0"><slot name="trailing" /></span>
    </RouterLink>
    <div v-else :class="inner">
      <span v-if="$slots.leading" class="shrink-0"><slot name="leading" /></span>
      <span class="min-w-0 flex-1 truncate font-semibold text-highlighted"><slot /></span>
      <span v-if="$slots.meta" class="shrink-0 text-sm text-muted"><slot name="meta" /></span>
      <span v-if="$slots.trailing" class="shrink-0"><slot name="trailing" /></span>
    </div>
  </li>
</template>

<script setup lang="ts">
import { RouterLink, type RouteLocationRaw } from 'vue-router'

defineProps<{ to?: RouteLocationRaw }>()

const inner = 'flex items-center gap-3 px-4 py-3'
const linkClass =
  'hover:bg-(--nb-soft) focus-visible:outline-2 focus-visible:[--nb-focus-offset:-2px] focus-visible:outline-primary'
</script>

<template>
  <li>
    <component
      :is="to ? RouterLink : 'div'"
      v-bind="to ? { to } : {}"
      :class="[inner, to ? linkClass : '']"
    >
      <span v-if="$slots.leading" class="shrink-0"><slot name="leading" /></span>
      <span data-title class="min-w-0 flex-1 truncate font-semibold text-highlighted"><slot /></span>
      <span v-if="$slots.meta" class="shrink-0 text-sm text-muted"><slot name="meta" /></span>
      <span v-if="$slots.trailing" class="shrink-0"><slot name="trailing" /></span>
    </component>
  </li>
</template>

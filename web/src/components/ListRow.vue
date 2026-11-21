<script setup lang="ts">
import { RouterLink, type RouteLocationRaw } from 'vue-router'

defineProps<{ to?: RouteLocationRaw }>()

const inner = 'flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 sm:flex-nowrap'
const linkClass =
  'hover:bg-(--nb-soft) focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary'
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
      <span
        v-if="$slots.meta"
        class="order-last max-w-full basis-full text-sm text-muted sm:order-none sm:shrink-0 sm:basis-auto"
      ><slot name="meta" /></span>
      <span v-if="$slots.trailing" class="shrink-0"><slot name="trailing" /></span>
    </component>
  </li>
</template>

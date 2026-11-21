<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { useCurrentCrumbs } from '@/lib/crumbs'

defineProps<{ title?: string }>()
const crumbs = useCurrentCrumbs()
</script>

<template>
  <header
    data-topbar
    class="sticky top-0 z-20 h-14 border-b border-default bg-default"
  >
    <div class="mx-auto flex h-full w-full max-w-7xl items-center gap-3 px-2 sm:px-8">
      <slot name="leading" />
      <nav v-if="crumbs?.length" aria-label="Breadcrumb" class="min-w-0">
        <ol class="flex min-w-0 items-center gap-1.5 text-sm">
          <li
            v-for="(c, i) in crumbs"
            :key="i"
            class="flex min-w-0 items-center gap-1.5"
            :class="{ 'max-sm:hidden': i < crumbs.length - 1 }"
          >
            <span v-if="i > 0" class="hidden text-muted sm:inline" aria-hidden="true">/</span>
            <span
              v-if="i === crumbs.length - 1"
              aria-current="page"
              class="truncate font-semibold text-highlighted"
              >{{ c.label }}</span
            >
            <RouterLink
              v-else-if="c.to"
              :to="c.to"
              class="truncate rounded-[4px] text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
              >{{ c.label }}</RouterLink
            >
            <span v-else class="truncate text-muted">{{ c.label }}</span>
          </li>
        </ol>
      </nav>
      <p v-else-if="title" class="min-w-0 truncate text-sm font-semibold text-highlighted">
        {{ title }}
      </p>
      <div class="ml-auto flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </div>
  </header>
</template>

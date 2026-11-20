<script setup lang="ts">
import { RouterLink, useRoute, useRouter } from 'vue-router'
import type { NavItem } from '@/router/nav'

const props = defineProps<{ items: NavItem[]; ink?: boolean }>()
const route = useRoute()
const router = useRouter()

function active(item: NavItem): boolean {
  const href = router.resolve({ name: item.name, params: route.params }).path
  return route.path === href || route.path.startsWith(`${href}/`)
}

const base =
  'flex items-center rounded-md px-2.5 py-1.5 text-sm transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2'
const tone = (on: boolean) =>
  props.ink
    ? on
      ? 'bg-ink-800 text-white font-semibold focus-visible:outline-white'
      : 'hover:bg-ink-900 hover:text-white focus-visible:outline-white'
    : on
      ? 'bg-elevated text-highlighted font-semibold focus-visible:outline-primary'
      : 'hover:bg-elevated/70 hover:text-highlighted focus-visible:outline-primary'
</script>

<template>
  <nav>
    <ul class="flex flex-col gap-0.5">
      <li v-for="item in items" :key="String(item.name)">
        <RouterLink
          :to="{ name: item.name, params: route.params }"
          :class="[base, tone(active(item))]"
          :aria-current="active(item) ? 'page' : undefined"
        >
          {{ item.label }}
        </RouterLink>
      </li>
    </ul>
  </nav>
</template>

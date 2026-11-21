<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UIcon from '@nuxt/ui/components/Icon.vue'
import { isActive, type NavItem } from '@/router/nav'

const props = defineProps<{ items: NavItem[]; ink?: boolean }>()
const route = useRoute()
const router = useRouter()

const active = (item: NavItem) => isActive(router, route, item)

const groups = computed(() => {
  const out: { title: string | null; items: NavItem[] }[] = []
  for (const item of props.items) {
    const last = out[out.length - 1]
    if (last && last.title === item.group) last.items.push(item)
    else out.push({ title: item.group, items: [item] })
  }
  return out
})

const base =
  'flex items-center gap-2.5 rounded-[8px] px-2.5 py-1.5 text-sm transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2'
const tone = (on: boolean) =>
  props.ink
    ? on
      ? 'bg-(--nb-ink-active) text-white font-semibold focus-visible:outline-white'
      : 'hover:bg-ink-900 hover:text-white focus-visible:outline-white'
    : on
      ? 'bg-elevated text-highlighted font-semibold ring-1 ring-default focus-visible:outline-primary'
      : 'hover:bg-elevated/70 hover:text-highlighted focus-visible:outline-primary'
const iconTone = (on: boolean) =>
  props.ink ? (on ? 'text-white' : '') : on ? 'text-highlighted' : 'text-muted'
const headingTone = computed(() => (props.ink ? 'text-ink-300' : 'text-muted'))
const soonTone = computed(() =>
  props.ink ? 'bg-ink-900 text-ink-300' : 'bg-elevated text-muted ring-1 ring-default',
)
</script>

<template>
  <nav class="flex flex-col gap-4">
    <div v-for="(group, gi) in groups" :key="group.title ?? `group-${gi}`">
      <p v-if="group.title" class="mb-1 px-2.5 text-xs font-medium" :class="headingTone">
        {{ group.title }}
      </p>
      <ul class="flex flex-col gap-0.5">
        <li
          v-for="item in group.items"
          :key="item.name === null ? `soon-${item.label}` : String(item.name)"
          :aria-disabled="item.soon ? 'true' : undefined"
        >
          <div v-if="item.soon" :class="[base, 'cursor-default opacity-70']">
            <UIcon :name="item.icon" class="size-4 shrink-0" aria-hidden="true" />
            <span>{{ item.label }}</span>
            <span class="ml-auto rounded-full px-1.5 py-0.5 text-xs" :class="soonTone">Soon</span>
          </div>
          <RouterLink
            v-else
            :to="{ name: item.name!, params: route.params }"
            :class="[base, tone(active(item))]"
            :aria-current="active(item) ? 'page' : undefined"
          >
            <UIcon
              :name="item.icon"
              class="size-4 shrink-0"
              :class="iconTone(active(item))"
              aria-hidden="true"
            />
            {{ item.label }}
          </RouterLink>
        </li>
      </ul>
    </div>
  </nav>
</template>

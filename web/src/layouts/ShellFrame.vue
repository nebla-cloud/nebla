<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import USlideover from '@nuxt/ui/components/Slideover.vue'

const props = defineProps<{ ink?: boolean; label: string }>()
const route = useRoute()
const open = ref(false)

watch(
  () => route.fullPath,
  () => {
    open.value = false
  },
)

const sidebar = computed(() =>
  props.ink
    ? 'bg-ink-950 text-ink-300 dark:border-r dark:border-ink-800'
    : 'bg-muted text-toned border-r border-default',
)
const topbar = computed(() =>
  props.ink
    ? 'bg-ink-950 text-white border-b border-ink-800'
    : 'bg-default text-highlighted border-b border-default',
)
</script>

<template>
  <div class="min-h-dvh lg:grid lg:grid-cols-[15rem_minmax(0,1fr)]">
    <aside
      class="sticky top-0 hidden h-dvh flex-col overflow-y-auto lg:flex"
      :class="sidebar"
      :aria-label="label"
    >
      <slot name="sidebar" />
    </aside>

    <div class="min-w-0">
      <header class="sticky top-0 z-20 flex h-14 items-center gap-2 px-2 lg:hidden" :class="topbar">
        <button
          type="button"
          class="grid size-10 place-items-center rounded-md hover:bg-current/10 focus-visible:outline-2 focus-visible:outline-current"
          aria-label="Open menu"
          :aria-expanded="open"
          @click="open = true"
        >
          <svg viewBox="0 0 20 20" class="size-5" aria-hidden="true">
            <path
              d="M3 5.5h14M3 10h14M3 14.5h14"
              stroke="currentColor"
              stroke-width="1.75"
              stroke-linecap="round"
            />
          </svg>
        </button>
        <div class="flex min-w-0 items-center gap-2">
          <slot name="topbar" />
        </div>
      </header>

      <main class="mx-auto w-full max-w-6xl px-4 py-6 sm:px-8 lg:px-10 lg:py-10">
        <RouterView />
      </main>
    </div>

    <USlideover
      v-model:open="open"
      side="left"
      :title="label"
      :ui="{ content: ['flex w-72 max-w-[85vw] flex-col overflow-y-auto', sidebar] }"
    >
      <template #content>
        <slot name="sidebar" />
      </template>
    </USlideover>
  </div>
</template>

<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import USlideover from '@nuxt/ui/components/Slideover.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import { isActive, type NavItem } from '@/router/nav'
import TopBar from './TopBar.vue'

const props = defineProps<{
  ink?: boolean
  label: string
  nav?: NavItem[]
  side: 'project' | 'admin'
}>()
const route = useRoute()
const router = useRouter()
const open = ref(false)

watch(
  () => route.fullPath,
  () => {
    open.value = false
  },
)

const title = computed(() => props.nav?.find((i) => isActive(router, route, i))?.label)

const sidebar = computed(() =>
  props.ink
    ? 'bg-ink-950 text-ink-300 [--nb-focus:var(--color-ink-100)] dark:border-r dark:border-ink-800'
    : 'bg-(--nb-sidebar) text-toned border-r border-default',
)
</script>

<template>
  <div class="min-h-dvh lg:grid lg:grid-cols-[14.5rem_minmax(0,1fr)]">
    <aside
      class="sticky top-0 hidden h-dvh min-w-0 flex-col overflow-y-auto lg:flex"
      :class="sidebar"
      :aria-label="label"
    >
      <slot name="sidebar" />
    </aside>

    <div class="min-w-0">
      <TopBar :title="title">
        <template #leading>
          <USlideover
            v-model:open="open"
            side="left"
            :title="label"
            :ui="{ content: ['flex w-72 max-w-[85vw] flex-col overflow-y-auto', sidebar] }"
          >
            <button
              type="button"
              class="grid size-10 shrink-0 place-items-center rounded-[8px] text-highlighted hover:bg-elevated focus-visible:outline-2 focus-visible:outline-primary lg:hidden"
              aria-label="Open menu"
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
            <template #content>
              <slot name="sidebar" />
            </template>
          </USlideover>
          <div class="flex min-w-0 max-w-[45%] shrink items-center gap-2 text-highlighted lg:hidden">
            <slot name="topbar" />
          </div>
          <span v-if="title" class="text-muted lg:hidden" aria-hidden="true">/</span>
        </template>
        <template #actions>
          <CommandPalette :side="side" />
          <slot name="actions" />
        </template>
      </TopBar>

      <main class="mx-auto w-full max-w-7xl px-4 py-6 sm:px-8 lg:py-8">
        <RouterView />
      </main>
    </div>
  </div>
</template>

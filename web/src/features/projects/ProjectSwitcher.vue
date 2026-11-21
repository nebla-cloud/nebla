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
import { computed } from 'vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import { useCurrentProject } from '@/lib/project'
import { roleLabel } from '@/lib/roles'
import { useProjects } from './queries'

const { projectId, project } = useCurrentProject()
const { data: projects } = useProjects()

const name = computed(() => project.value?.name ?? 'Project')
const role = computed(() => (project.value ? roleLabel(project.value.role) : ''))
const initial = computed(() => name.value.trim().charAt(0).toUpperCase() || '?')

const items = computed<DropdownMenuItem[][]>(() => [
  (projects.value ?? []).map((p) => ({
    label: p.name,
    to: `/p/${p.id}/servers`,
    class: p.id === projectId.value ? 'font-semibold text-highlighted' : undefined,
  })),
  [{ label: 'New project', to: { name: 'projects', query: { new: '1' } } }],
])
</script>

<template>
  <UDropdownMenu :items="items" :content="{ align: 'start' }" :ui="{ content: 'w-56' }">
    <button
      type="button"
      class="flex w-full min-w-0 items-center gap-2.5 rounded-[0.625rem] border border-default bg-elevated p-2 text-left hover:bg-accented/50 focus-visible:outline-2 focus-visible:outline-primary"
    >
      <span
        class="grid size-7 shrink-0 place-items-center rounded-[8px] bg-(--ui-bg-muted) text-xs font-bold text-highlighted ring-1 ring-default"
        aria-hidden="true"
        >{{ initial }}</span
      >
      <span class="min-w-0 flex-1">
        <span class="sr-only">Switch project</span>
        <span class="block truncate text-sm font-semibold text-highlighted">{{ name }}</span>
        <span v-if="role" class="block truncate text-xs text-muted">{{ role }}</span>
      </span>
      <svg viewBox="0 0 20 20" class="size-4 shrink-0 text-muted" aria-hidden="true">
        <path
          d="m6 8 4 4 4-4"
          fill="none"
          stroke="currentColor"
          stroke-width="1.75"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
    </button>
  </UDropdownMenu>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import type { DropdownMenuItem } from '@nuxt/ui/components/DropdownMenu.vue'
import BrandMark from '@/components/BrandMark.vue'
import { useCurrentProject } from '@/lib/project'
import { useProjects } from './queries'

const { projectId, project } = useCurrentProject()
const { data: projects } = useProjects()

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
      class="flex w-full min-w-0 items-center gap-2.5 rounded-md px-2.5 py-2 text-left hover:bg-elevated/70 focus-visible:outline-2 focus-visible:outline-primary"
      aria-label="Switch project"
    >
      <BrandMark :size="22" />
      <span class="min-w-0 flex-1 truncate font-bold tracking-tight text-highlighted">
        {{ project?.name ?? 'Project' }}
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

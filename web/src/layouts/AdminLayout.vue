<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import BrandMark from '@/components/BrandMark.vue'
import { useProjects } from '@/features/projects/queries'
import { readLastProject } from '@/lib/project'
import { useNav } from '@/router/nav'
import ShellFrame from './ShellFrame.vue'
import SideNav from './SideNav.vue'

const nav = useNav('admin')
const { data: projects } = useProjects()
const lastId = readLastProject()
const last = computed(() => projects.value?.find((p) => p.id === lastId))
</script>

<template>
  <ShellFrame ink label="Admin menu">
    <template #sidebar>
      <div class="flex flex-1 flex-col gap-4 p-3">
        <div class="flex items-center gap-2.5 px-2.5 py-2">
          <BrandMark :size="22" />
          <span class="font-bold tracking-tight text-white">Admin</span>
        </div>
        <SideNav :items="nav" ink />
        <RouterLink
          :to="last ? `/p/${last.id}/servers` : { name: 'projects' }"
          class="mt-auto truncate rounded-md px-2.5 py-1.5 text-sm text-ink-300 hover:bg-ink-900 hover:text-white focus-visible:outline-2 focus-visible:outline-white"
        >
          {{ last ? `Back to ${last.name}` : 'Go to projects' }}
        </RouterLink>
      </div>
    </template>
    <template #topbar>
      <BrandMark :size="20" />
      <span class="font-bold tracking-tight">Admin</span>
    </template>
  </ShellFrame>
</template>

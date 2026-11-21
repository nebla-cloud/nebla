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
import { RouterLink } from 'vue-router'
import UIcon from '@nuxt/ui/components/Icon.vue'
import BrandMark from '@/components/BrandMark.vue'
import Lockup from '@/components/Lockup.vue'
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
  <ShellFrame ink side="admin" label="Admin menu" :nav="nav">
    <template #sidebar>
      <div class="flex min-w-0 flex-1 flex-col gap-3.5 px-3 py-4">
        <!-- The ink sidebar is dark in both modes, so the lockup is drawn in white. -->
        <div
          class="flex min-w-0 items-center gap-2.5 px-2 py-1 [--ui-text-highlighted:var(--color-white)]"
        >
          <Lockup :height="22" />
          <span class="shrink-0 font-bold tracking-tight text-white">Admin</span>
        </div>
        <SideNav :items="nav" ink />
        <RouterLink
          :to="last ? `/p/${last.id}/servers` : { name: 'projects' }"
          class="mt-auto flex min-w-0 items-center gap-2.5 rounded-[8px] px-2.5 py-1.5 text-sm hover:bg-ink-900 hover:text-white focus-visible:outline-2 focus-visible:outline-white"
        >
          <UIcon name="i-lucide-arrow-left" class="size-4 shrink-0" aria-hidden="true" />
          <span class="truncate">{{ last ? `Back to ${last.name}` : 'Go to projects' }}</span>
        </RouterLink>
      </div>
    </template>
    <template #topbar>
      <BrandMark :size="20" />
      <span class="font-bold tracking-tight">Admin</span>
    </template>
  </ShellFrame>
</template>

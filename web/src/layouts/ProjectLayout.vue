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
import { RouterLink } from 'vue-router'
import UIcon from '@nuxt/ui/components/Icon.vue'
import AccountMenu from '@/components/AccountMenu.vue'
import BrandMark from '@/components/BrandMark.vue'
import Lockup from '@/components/Lockup.vue'
import ProjectSwitcher from '@/features/projects/ProjectSwitcher.vue'
import { useCurrentProject } from '@/lib/project'
import { useSession } from '@/lib/session'
import { useNav } from '@/router/nav'
import ShellFrame from './ShellFrame.vue'
import SideNav from './SideNav.vue'

const { me } = useSession()
const { project } = useCurrentProject()
const nav = useNav('project')
</script>

<template>
  <ShellFrame side="project" label="Project menu" :nav="nav">
    <template #sidebar>
      <div class="flex min-w-0 flex-1 flex-col gap-3.5 px-3 py-4">
        <RouterLink
          to="/projects"
          class="flex max-w-full min-w-0 items-center self-start rounded-[8px] px-2 py-1 focus-visible:outline-2 focus-visible:outline-primary"
        >
          <Lockup :height="22" />
        </RouterLink>
        <ProjectSwitcher />
        <SideNav :items="nav" />
        <div class="mt-auto flex min-w-0 flex-col gap-1">
          <RouterLink
            v-if="me?.is_admin"
            to="/admin"
            class="flex items-center gap-2.5 rounded-[8px] px-2.5 py-1.5 text-sm hover:bg-elevated/70 hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
          >
            <UIcon
              name="i-lucide-shield-check"
              class="size-4 shrink-0 text-muted"
              aria-hidden="true"
            />
            Admin
          </RouterLink>
          <AccountMenu />
        </div>
      </div>
    </template>
    <template #topbar>
      <BrandMark :size="20" />
      <span class="truncate font-bold tracking-tight">{{ project?.name }}</span>
    </template>
  </ShellFrame>
</template>

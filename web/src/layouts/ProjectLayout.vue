<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AccountMenu from '@/components/AccountMenu.vue'
import BrandMark from '@/components/BrandMark.vue'
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
  <ShellFrame label="Project menu">
    <template #sidebar>
      <div class="flex flex-1 flex-col gap-4 p-3">
        <ProjectSwitcher />
        <SideNav :items="nav" />
        <div class="mt-auto flex flex-col gap-1">
          <RouterLink
            v-if="me?.is_admin"
            to="/admin"
            class="rounded-md px-2.5 py-1.5 text-sm hover:bg-elevated/70 hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
          >
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

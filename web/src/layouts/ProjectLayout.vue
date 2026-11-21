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
  <ShellFrame label="Project menu" :nav="nav">
    <template #sidebar>
      <div class="flex min-w-0 flex-1 flex-col gap-3.5 px-3 py-4">
        <RouterLink
          to="/projects"
          class="flex max-w-full min-w-0 items-center self-start rounded-md px-2 py-1 focus-visible:outline-2 focus-visible:outline-primary"
        >
          <Lockup :height="22" />
        </RouterLink>
        <ProjectSwitcher />
        <SideNav :items="nav" />
        <div class="mt-auto flex min-w-0 flex-col gap-1">
          <RouterLink
            v-if="me?.is_admin"
            to="/admin"
            class="flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm hover:bg-elevated/70 hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
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

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
import { computed, ref } from 'vue'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import UModal from '@nuxt/ui/components/Modal.vue'
import UCommandPalette from '@nuxt/ui/components/CommandPalette.vue'
import UIcon from '@nuxt/ui/components/Icon.vue'
import UKbd from '@nuxt/ui/components/Kbd.vue'
import type { CommandPaletteGroup, CommandPaletteItem } from '@nuxt/ui'
import { useServers } from '@/features/servers/queries'
import { useSshKeys } from '@/features/sshkeys/queries'
import { useCommandShortcut } from '@/lib/useCommandShortcut'
import { useSession } from '@/lib/session'
import { useNav, type NavItem } from '@/router/nav'

const props = defineProps<{ side: 'project' | 'admin' }>()

const open = ref(false)
useCommandShortcut(open)

const route = useRoute()
const router = useRouter()
const { me } = useSession()
const pages = useNav(props.side)
const adminPages = useNav('admin')
// Cache only: the palette never fetches. Admin routes reuse :projectId, so skip the project hooks there.
const cacheOnly = ref(false)
const onProject = props.side === 'project'
const servers = onProject ? useServers(undefined, cacheOnly).data : undefined
const keys = onProject ? useSshKeys(cacheOnly).data : undefined

type Nav = { userAgentData?: { platform?: string } }
const platform = (navigator as Navigator & Nav).userAgentData?.platform ?? navigator.platform
const shortcut = /mac|iphone|ipad/i.test(platform ?? '') ? '⌘K' : 'Ctrl K'

function go(to: RouteLocationRaw): void {
  open.value = false
  void router.push(to)
}

const projectParams = computed(() => ({ projectId: String(route.params.projectId ?? '') }))

function pageItems(items: NavItem[], side: 'project' | 'admin'): CommandPaletteItem[] {
  return items
    .filter((i) => !i.soon && i.name !== null)
    .map((i) => ({
      label: i.label,
      icon: i.icon,
      onSelect: () =>
        go(side === 'project' ? { name: i.name!, params: projectParams.value } : { name: i.name! }),
    }))
}

const groups = computed<CommandPaletteGroup[]>(() => {
  const out: CommandPaletteGroup[] = [
    { id: 'pages', label: 'Pages', items: pageItems(pages.value, props.side) },
  ]
  if (!onProject) return out
  out.push({
    id: 'servers',
    label: 'Servers',
    items: (servers?.value ?? []).map((s) => ({
      label: s.name,
      icon: 'i-lucide-server',
      onSelect: () =>
        go({ name: 'server', params: { ...projectParams.value, serverId: String(s.id) } }),
    })),
  })
  out.push({
    id: 'keys',
    label: 'SSH keys',
    items: (keys?.value ?? []).map((k) => ({
      label: k.name,
      icon: 'i-lucide-key-round',
      onSelect: () => go({ name: 'ssh-keys', params: projectParams.value }),
    })),
  })
  if (me.value?.is_admin) {
    out.push({ id: 'admin', label: 'Admin', items: pageItems(adminPages.value, 'admin') })
  }
  return out
})

const placeholder = computed(() =>
  props.side === 'project' ? 'Search pages, servers and keys' : 'Search pages',
)
</script>

<template>
  <UModal
    v-model:open="open"
    title="Search"
    description="Go to a page, server or SSH key."
    :ui="{ content: 'sm:max-w-xl' }"
  >
    <button
      type="button"
      aria-keyshortcuts="Meta+K Control+K"
      class="grid size-10 shrink-0 place-items-center rounded-[8px] text-muted hover:bg-elevated hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary sm:flex sm:h-[34px] sm:w-56 sm:items-center sm:gap-2 sm:rounded-[9px] sm:border sm:border-default sm:bg-muted sm:px-2.5 sm:text-sm sm:hover:bg-muted lg:w-64"
    >
      <UIcon name="i-lucide-search" class="size-4 shrink-0" aria-hidden="true" />
      <span class="sr-only sm:not-sr-only">Search</span>
      <UKbd aria-hidden="true" class="ml-auto hidden sm:inline-flex">{{ shortcut }}</UKbd>
    </button>
    <template #content>
      <UCommandPalette
        :groups="groups"
        :placeholder="placeholder"
        preserve-group-order
        class="h-80 max-h-[70dvh]"
      />
    </template>
  </UModal>
</template>

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
import { RouterLink, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UPopover from '@nuxt/ui/components/Popover.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import { useSession } from '@/lib/session'
import ColorModeToggle from './ColorModeToggle.vue'

const { me, logout } = useSession()
const router = useRouter()
const qc = useQueryClient()
const toast = useToast()

async function logOut() {
  try {
    await logout()
  } catch (e) {
    toast.add({ title: "Couldn't log out", description: errorMessage(e), color: 'error' })
    return
  }
  qc.clear()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <UPopover :content="{ side: 'top', align: 'start' }">
    <button
      type="button"
      class="flex w-full min-w-0 items-center gap-2.5 rounded-[10px] p-2 text-left text-sm hover:bg-elevated/70 focus-visible:outline-2 focus-visible:outline-primary"
    >
      <span
        class="grid size-7 shrink-0 place-items-center rounded-t-full rounded-b-[6px] bg-(image:--nb-gradient) text-xs font-bold text-(--nb-on-gradient)"
        aria-hidden="true"
      >
        {{ (me?.name || me?.email || '?').charAt(0).toUpperCase() }}
      </span>
      <span class="min-w-0 truncate text-toned">{{ me?.email }}</span>
    </button>
    <template #content>
      <div class="flex w-64 max-w-[calc(100vw-2rem)] flex-col gap-3 p-3">
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold text-highlighted">{{ me?.name || me?.email }}</p>
          <p v-if="me?.name" class="truncate text-sm text-muted">{{ me?.email }}</p>
        </div>
        <RouterLink
          to="/account"
          class="rounded-[8px] px-2 py-1.5 text-sm text-toned hover:bg-elevated/70 focus-visible:outline-2 focus-visible:outline-primary"
        >
          Account settings
        </RouterLink>
        <ColorModeToggle />
        <UButton color="neutral" variant="outline" block @click="logOut">Log out</UButton>
      </div>
    </template>
  </UPopover>
</template>

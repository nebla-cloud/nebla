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
      class="flex w-full min-w-0 items-center gap-2.5 rounded-lg p-2 text-left text-sm hover:bg-elevated/70 focus-visible:outline-2 focus-visible:outline-primary"
    >
      <span
        class="grid size-7 shrink-0 place-items-center rounded-t-full rounded-b-md bg-(image:--nb-gradient) text-xs font-bold text-(--nb-on-gradient)"
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
          class="rounded-md px-2 py-1.5 text-sm text-toned hover:bg-elevated/70 focus-visible:outline-2 focus-visible:outline-primary"
        >
          Account settings
        </RouterLink>
        <ColorModeToggle />
        <UButton color="neutral" variant="outline" block @click="logOut">Log out</UButton>
      </div>
    </template>
  </UPopover>
</template>

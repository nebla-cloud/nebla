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
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import type { NetworkGuestConfig } from '@/api/types'
import CommandBlock from '@/features/floating-ips/CommandBlock.vue'

defineProps<{ config: NetworkGuestConfig; serverName: string }>()
const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <UModal
    v-model:open="open"
    title="Run this on the server"
    description="New and rebuilt servers set this up automatically. For a running server, run these commands."
    :ui="{ content: 'sm:max-w-2xl' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <h3 class="text-sm text-muted">
          On <span class="font-semibold text-highlighted">{{ serverName }}</span>
        </h3>
        <p v-if="config.note" data-testid="guest-note" class="text-sm text-highlighted">
          {{ config.note }}
        </p>
        <CommandBlock
          v-if="config.apply"
          data-testid="guest-apply"
          label="Bring the network up now"
          :text="config.apply"
          :copy-label="`Copy the commands that bring the network up on ${serverName}`"
        />
        <CommandBlock
          v-if="config.persist"
          data-testid="guest-persist"
          label="Keep it after a reboot"
          :text="config.persist"
          :copy-label="`Copy the commands that keep the network on ${serverName}`"
        />
        <CommandBlock
          v-if="config.remove"
          data-testid="guest-remove"
          label="Take the network down"
          :text="config.remove"
          :copy-label="`Copy the commands that take the network down on ${serverName}`"
        />
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end">
        <UButton color="neutral" variant="outline" @click="open = false">Done</UButton>
      </div>
    </template>
  </UModal>
</template>

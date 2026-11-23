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
import type { GuestConfig } from '@/api/types'
import CommandBlock from './CommandBlock.vue'

defineProps<{ config: GuestConfig }>()
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
      <div class="flex flex-col gap-5">
        <section v-if="config.apply" class="flex flex-col gap-3">
          <h3 class="text-sm text-muted">
            On <span class="font-semibold text-highlighted">{{ config.server }}</span>
          </h3>
          <CommandBlock
            data-testid="guest-apply"
            label="Use the address now"
            :text="config.apply"
            :copy-label="`Copy the commands that make ${config.server} use the address`"
          />
          <CommandBlock
            v-if="config.persist"
            data-testid="guest-persist"
            label="Make it permanent"
            :text="config.persist"
            :copy-label="`Copy the commands that keep the address on ${config.server}`"
          />
        </section>
        <section v-if="config.previous_server && config.remove" class="flex flex-col gap-3">
          <h3 class="text-sm font-semibold text-highlighted">
            Run this on {{ config.previous_server }} to stop using the address
          </h3>
          <CommandBlock
            data-testid="guest-remove"
            label="Stop using the address"
            :text="config.remove"
            :copy-label="`Copy the commands that stop ${config.previous_server} using the address`"
          />
        </section>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end">
        <UButton color="neutral" variant="outline" @click="open = false">Done</UButton>
      </div>
    </template>
  </UModal>
</template>

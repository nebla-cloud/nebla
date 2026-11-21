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
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import { isTransitional } from '@/lib/status'

const props = defineProps<{ server: Server; busy?: boolean }>()
const emit = defineEmits<{ started: [action: Action] }>()

type Power = 'poweron' | 'poweroff' | 'shutdown' | 'reboot' | 'reset'

const calls = {
  poweron: (p: { path: { id: number } }) =>
    api.POST('/servers/{id}/actions/poweron', { params: p }),
  poweroff: (p: { path: { id: number } }) =>
    api.POST('/servers/{id}/actions/poweroff', { params: p }),
  shutdown: (p: { path: { id: number } }) =>
    api.POST('/servers/{id}/actions/shutdown', { params: p }),
  reboot: (p: { path: { id: number } }) => api.POST('/servers/{id}/actions/reboot', { params: p }),
  reset: (p: { path: { id: number } }) => api.POST('/servers/{id}/actions/reset', { params: p }),
}

const toast = useToast()
const pending = ref(false)

const locked = computed(() => props.busy || pending.value || isTransitional(props.server.status))
const showRunning = computed(
  () => props.server.status === 'running' || isTransitional(props.server.status),
)
const showOff = computed(() => props.server.status === 'off')
const showError = computed(() => props.server.status === 'error')
const missing = computed(() => props.server.status === 'missing')

async function run(kind: Power) {
  pending.value = true
  try {
    const params = { path: { id: props.server.id } }
    const res = await calls[kind](params)
    emit('started', unwrap(res).action)
  } catch (e) {
    toast.add({
      title: `Couldn't update ${props.server.name}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

const overflow = computed(() => [
  [
    { label: 'Power off', icon: 'i-lucide-power', onSelect: () => void run('poweroff') },
    { label: 'Reset', icon: 'i-lucide-rotate-ccw', onSelect: () => void run('reset') },
  ],
])
</script>

<template>
  <div class="flex items-center gap-1.5">
    <template v-if="showRunning">
      <UButton color="neutral" variant="outline" :disabled="locked" @click="run('shutdown')">
        Shut down
      </UButton>
      <UButton color="neutral" variant="outline" :disabled="locked" @click="run('reboot')">
        Reboot
      </UButton>
      <UDropdownMenu :items="overflow" :content="{ align: 'end' }" :disabled="locked">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-ellipsis"
          aria-label="More power actions"
          :disabled="locked"
        />
      </UDropdownMenu>
    </template>
    <UButton
      v-else-if="showOff"
      color="neutral"
      variant="outline"
      :disabled="locked"
      @click="run('poweron')"
    >
      Power on
    </UButton>
    <template v-else-if="showError">
      <UButton color="neutral" variant="outline" :disabled="locked" @click="run('poweron')">
        Power on
      </UButton>
      <UButton color="neutral" variant="outline" :disabled="locked" @click="run('poweroff')">
        Power off
      </UButton>
    </template>
    <UButton
      v-else-if="missing"
      color="neutral"
      variant="outline"
      disabled
      title="Nebla can't find this server right now"
    >
      Power on
    </UButton>
  </div>
</template>

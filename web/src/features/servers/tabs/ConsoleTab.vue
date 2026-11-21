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
import { computed, inject, nextTick, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import type { Action, Server } from '@/api/types'
import Panel from '@/components/Panel.vue'
import { useCurrentProject } from '@/lib/project'
import ConsolePanel from '../console/ConsolePanel.vue'
import PowerActions from '../PowerActions.vue'
import { serverBusyKey } from '../shell'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const router = useRouter()
const toast = useToast()
const { projectId } = useCurrentProject()
const busy = inject(serverBusyKey, undefined)

const running = computed(() => props.server.status === 'running')
// 'open' while the console lives in another window; 'back' once that window closes.
const popped = ref<'no' | 'open' | 'back'>('no')
let popup: Window | null = null
let timer: ReturnType<typeof setInterval> | undefined

function stopWatching() {
  if (timer !== undefined) clearInterval(timer)
  timer = undefined
}

async function popOut() {
  popped.value = 'open'
  // Let the inline session close before the window asks for its own grant.
  await nextTick()
  const href = router.resolve({
    name: 'server-console-window',
    params: { projectId: projectId.value, serverId: props.server.id },
  }).href
  popup = window.open(href, `nebla-console-${props.server.id}`, 'popup,width=1024,height=800')
  if (!popup) {
    popped.value = 'back'
    toast.add({ title: 'Your browser blocked the new window', color: 'error' })
    return
  }
  stopWatching()
  timer = setInterval(() => {
    if (!popup || popup.closed) {
      stopWatching()
      popup = null
      popped.value = 'back'
    }
  }, 1000)
}

function bringBack() {
  stopWatching()
  popup?.close()
  popup = null
  popped.value = 'no'
}

onBeforeUnmount(stopWatching)
</script>

<template>
  <div class="flex flex-col gap-3">
    <Panel v-if="!running">
      <div class="flex flex-col items-start gap-3">
        <p class="text-sm text-muted">
          {{ server.name }} isn't running. Power it on to use the console.
        </p>
        <PowerActions :server="server" :busy="busy" @started="emit('started', $event)" />
      </div>
    </Panel>

    <Panel v-else-if="popped === 'open'" title="Open in another window">
      <div class="flex flex-col items-start gap-3">
        <p class="text-sm text-muted">Only one console session runs at a time.</p>
        <UButton color="neutral" variant="outline" @click="bringBack">Bring it back here</UButton>
      </div>
    </Panel>

    <ConsolePanel v-else :key="server.id" :server-id="server.id" :auto-connect="popped === 'no'">
      <template #actions>
        <UButton
          size="sm"
          color="neutral"
          variant="outline"
          icon="i-lucide-external-link"
          @click="popOut"
        >
          Pop out
        </UButton>
      </template>
    </ConsolePanel>
  </div>
</template>

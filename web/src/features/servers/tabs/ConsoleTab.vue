<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import type { Action, Server } from '@/api/types'
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
    <div
      v-if="!running"
      class="flex flex-col items-start gap-3 rounded-lg border border-default bg-default p-4"
    >
      <p class="text-sm text-muted">
        {{ server.name }} isn't running. Power it on to use the console.
      </p>
      <PowerActions :server="server" :busy="busy" @started="emit('started', $event)" />
    </div>

    <div
      v-else-if="popped === 'open'"
      class="flex flex-col items-start gap-3 rounded-lg border border-default bg-default p-4"
    >
      <p class="font-medium text-highlighted">Open in another window</p>
      <p class="text-sm text-muted">Only one console session runs at a time.</p>
      <UButton color="neutral" variant="outline" @click="bringBack">Bring it back here</UButton>
    </div>

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

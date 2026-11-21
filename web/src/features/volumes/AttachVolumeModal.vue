<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action, Volume } from '@/api/types'
import { useServers } from '@/features/servers/queries'
import { selectClass } from '@/lib/ui'
import { attachVolume } from './queries'
import { serverChoices } from './serverPicker'

const props = defineProps<{ volume: Volume }>()
const emit = defineEmits<{ started: [action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const { data: servers, isPending } = useServers()
const choices = computed(() => serverChoices(servers.value ?? [], props.volume.location.id))
const serverId = ref<number | null>(null)
const error = ref<string | null>(null)
const pending = ref(false)

watch(open, (v) => {
  if (v) return
  serverId.value = null
  error.value = null
})

async function submit() {
  error.value = null
  if (serverId.value === null) {
    error.value = 'Pick a server.'
    return
  }
  pending.value = true
  try {
    emit('started', await attachVolume(props.volume.id, serverId.value))
    open.value = false
  } catch (e) {
    // The API has the final say, such as a server on another host (volume_server_mismatch).
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="`Attach ${volume.name}`"
    :description="`Servers in ${volume.location.name} can take this volume.`"
  >
    <template #body>
      <form id="attach-volume" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <p v-if="isPending" class="text-muted">Loading servers…</p>
        <p v-else-if="!choices.length" class="text-muted">This project has no servers yet.</p>
        <UFormField v-else label="Server" name="server">
          <select v-model="serverId" name="server" :class="selectClass">
            <option :value="null" disabled>Pick a server</option>
            <option
              v-for="c in choices"
              :key="c.server.id"
              :value="c.server.id"
              :disabled="c.disabledReason !== null"
            >
              {{ c.server.name }}{{ c.disabledReason ? ` (${c.disabledReason})` : '' }}
            </option>
          </select>
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="attach-volume" :loading="pending">Attach</UButton>
      </div>
    </template>
  </UModal>
</template>

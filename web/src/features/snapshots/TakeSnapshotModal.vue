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
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UCheckbox from '@nuxt/ui/components/Checkbox.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action, Server, Snapshot } from '@/api/types'
import { takeWarning } from './copy'
import { takeSnapshot } from './queries'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [snapshot: Snapshot, action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const NAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

// The server's name and the UTC time, trimmed like the API's default.
function defaultName(): string {
  const d = new Date().toISOString()
  const suffix = `-${d.slice(0, 10).replaceAll('-', '')}-${d.slice(11, 16).replace(':', '')}`
  return `${props.server.name.slice(0, 62 - suffix.length).replace(/-+$/, '')}${suffix}`
}

const name = ref(defaultName())
const description = ref('')
const forceStop = ref(false)
const nameError = ref<string | null>(null)
const error = ref<string | null>(null)
const pending = ref(false)
const running = computed(() => props.server.status === 'running')

watch(open, (v) => {
  if (v) {
    name.value = defaultName()
    description.value = ''
    forceStop.value = false
  }
  nameError.value = error.value = null
})

async function submit() {
  nameError.value = error.value = null
  const n = name.value.trim()
  if (!NAME.test(n)) {
    nameError.value =
      'Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.'
    return
  }
  pending.value = true
  try {
    const res = await takeSnapshot({
      server: String(props.server.id),
      name: n,
      ...(description.value.trim() ? { description: description.value.trim() } : {}),
      ...(running.value && forceStop.value ? { force_stop: true } : {}),
    })
    emit('started', res.snapshot, res.action)
    open.value = false
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Take snapshot"
    :description="`Saves ${server.name}'s boot disk. Attached volumes aren't included.`"
  >
    <template #body>
      <form id="take-snapshot" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <UAlert
          v-if="running"
          color="warning"
          variant="subtle"
          icon="i-lucide-triangle-alert"
          :title="takeWarning(server.name)"
        />
        <UFormField label="Name" name="name" :error="nameError ?? undefined">
          <UInput v-model="name" name="name" autocomplete="off" class="w-full" />
        </UFormField>
        <UFormField label="Description" name="description" help="Optional.">
          <UInput v-model="description" name="description" autocomplete="off" class="w-full" />
        </UFormField>
        <UCheckbox
          v-if="running"
          v-model="forceStop"
          name="force_stop"
          label="Force stop if it doesn't shut down"
        />
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="take-snapshot" :loading="pending">Take snapshot</UButton>
      </div>
    </template>
  </UModal>
</template>
